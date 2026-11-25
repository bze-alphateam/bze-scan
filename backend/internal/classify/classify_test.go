package classify_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/classify"
)

var categories = map[string]bool{
	classify.Sent: true, classify.Received: true, classify.Staking: true, classify.DEX: true,
	classify.Governance: true, classify.Tokens: true, classify.Rewards: true, classify.Burner: true,
	classify.CrossChain: true, classify.Other: true,
}

// Every message type the chain accepts is either classified or deliberately
// left out, and every entry names a type the chain really registers, except
// the retired ones, which it no longer registers.
func TestEveryRegisteredMessageIsClassifiedOrAllowListed(t *testing.T) {
	codec, err := chain.NewCodec()
	require.NoError(t, err)
	registered := map[string]bool{}
	for _, u := range codec.MsgTypeURLs() {
		registered[u] = true
	}
	retired := map[string]bool{}
	for _, u := range classify.Retired {
		retired[u] = true
		assert.False(t, registered[u], "%s is retired but the chain codec still registers it", u)
	}

	classified := map[string]bool{}
	for _, m := range classify.Messages() {
		classified[m.TypeURL] = true
		assert.True(t, registered[m.TypeURL] || retired[m.TypeURL], "%s is classified but not registered by the chain", m.TypeURL)
	}
	allowed := map[string]bool{}
	for _, u := range classify.Unclassified {
		allowed[u] = true
		assert.True(t, registered[u] || retired[u], "%s is allow-listed but not registered by the chain", u)
		assert.False(t, classified[u], "%s is both classified and allow-listed", u)
	}
	for u := range registered {
		assert.True(t, classified[u] || allowed[u], "%s has no classification entry and is not allow-listed", u)
	}
}

func TestEntriesAreWellFormed(t *testing.T) {
	for _, m := range classify.Messages() {
		assert.NotEmpty(t, m.Kind, m.TypeURL)
		assert.True(t, categories[m.SignerCategory], "%s: signer category %q", m.TypeURL, m.SignerCategory)
		assert.True(t, categories[m.ParticipantCategory], "%s: participant category %q", m.TypeURL, m.ParticipantCategory)
		if m.CounterpartyAttr != "" {
			assert.Contains(t, m.CounterpartyAttr, ".", "%s: counterparty is <event>.<attribute>", m.TypeURL)
		}
	}
	for _, e := range classify.BlockEvents() {
		assert.NotEmpty(t, e.Kind, e.EventType)
		assert.True(t, categories[e.Category], "%s: category %q", e.EventType, e.Category)
		assert.NotNil(t, e.AddressAttrs, e.EventType)
	}
}

func TestLookup(t *testing.T) {
	m := classify.Lookup("/cosmos.bank.v1beta1.MsgSend")
	assert.Equal(t, classify.MessageKind{
		TypeURL: "/cosmos.bank.v1beta1.MsgSend", Kind: "send",
		SignerCategory: classify.Sent, ParticipantCategory: classify.Received, CounterpartyAttr: "transfer.recipient",
	}, m)

	m = classify.Lookup("/bze.future.MsgSomething")
	assert.Equal(t, "/bze.future.MsgSomething", m.TypeURL)
	assert.Equal(t, classify.KindOther, m.Kind)
	assert.Equal(t, classify.Other, m.SignerCategory)
	assert.Equal(t, classify.Other, m.ParticipantCategory)

	assert.Equal(t, classify.KindOther, classify.Lookup("/cosmos.gov.v1.MsgUpdateParams").Kind, "allow-listed types are other")
}

func TestLookupBlockEvent(t *testing.T) {
	e, ok := classify.LookupBlockEvent("bze.tradebin.OrderExecutedEvent")
	require.True(t, ok)
	assert.Equal(t, "dex_fill", e.Kind)
	assert.Equal(t, []string{"maker", "taker"}, e.AddressAttrs)

	for _, routine := range []string{"mint", "commission", "rewards", "coin_spent", "transfer", "liveness"} {
		_, ok := classify.LookupBlockEvent(routine)
		assert.False(t, ok, "%s is not stored", routine)
	}
}

func TestListsAreSortedCopies(t *testing.T) {
	ms := classify.Messages()
	for i := 1; i < len(ms); i++ {
		assert.Less(t, ms[i-1].TypeURL, ms[i].TypeURL)
	}
	ms[0].Kind = "changed"
	assert.NotEqual(t, "changed", classify.Messages()[0].Kind)
}

type execCall struct {
	sql  string
	args []any
}

type mockExecer struct {
	calls  []execCall
	failOn string
}

func (m *mockExecer) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	m.calls = append(m.calls, execCall{sql, args})
	if m.failOn != "" && strings.Contains(sql, m.failOn) {
		return pgconn.CommandTag{}, errors.New("boom")
	}
	return pgconn.CommandTag{}, nil
}

func TestMirrorUpsertsDeletesStaleAndReclassifies(t *testing.T) {
	db := &mockExecer{}
	require.NoError(t, classify.Mirror(context.Background(), db))
	require.Len(t, db.calls, 5)

	upsert := db.calls[0]
	assert.Contains(t, upsert.sql, "INSERT INTO explorer.message_kinds")
	urls := upsert.args[0].([]string)
	assert.Len(t, urls, len(classify.Messages()))
	for _, col := range upsert.args {
		assert.Len(t, col, len(urls), "one value per entry in every column")
	}
	assert.Contains(t, db.calls[1].sql, "DELETE FROM explorer.message_kinds")
	assert.Equal(t, urls, db.calls[1].args[0])

	events := db.calls[2]
	assert.Contains(t, events.sql, "INSERT INTO explorer.block_event_kinds")
	types := events.args[0].([]string)
	assert.Len(t, types, len(classify.BlockEvents()))
	attrs := events.args[3].([]string)
	assert.Contains(t, attrs, `["maker","taker"]`)
	assert.Contains(t, attrs, `[]`)
	assert.Contains(t, db.calls[3].sql, "DELETE FROM explorer.block_event_kinds")

	assert.Contains(t, db.calls[4].sql, "explorer.reclassify_unknown()")
}

func TestMirrorStopsAtTheFirstError(t *testing.T) {
	db := &mockExecer{failOn: "block_event_kinds"}
	err := classify.Mirror(context.Background(), db)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
	assert.Len(t, db.calls, 3)
}

// The bze.* block events are exactly the typed events the chain's keepers
// emit outside a message handler: EndBlock, BeginBlock and epoch hooks.
func TestBZEBlockEventsAreTheChainsBlockLevelEmitters(t *testing.T) {
	var got []string
	for _, e := range classify.BlockEvents() {
		if strings.HasPrefix(e.EventType, "bze.") {
			got = append(got, e.EventType)
		}
	}
	assert.Equal(t, []string{
		"bze.burner.CoinsBurnedEvent",                  // periodic burn, raffle clean-up
		"bze.burner.RaffleFinishedEvent",               // raffle clean-up hook
		"bze.burner.RaffleLostEvent",                   // EndBlock draw
		"bze.burner.RaffleWinnerEvent",                 // EndBlock draw
		"bze.dao.EventAdminChanged",                    // gov-executed DAO message
		"bze.dao.EventAdminExecuted",                   // gov-executed DAO message
		"bze.dao.EventAdminNominated",                  // gov-executed DAO message
		"bze.dao.EventAdminNominationWithdrawn",        // gov-executed DAO message
		"bze.dao.EventBallotReduced",                   // gov-executed DAO message
		"bze.dao.EventDaoConfigUpdated",                // gov-executed DAO message
		"bze.dao.EventDaoCreated",                      // gov-executed DAO message
		"bze.dao.EventDaoElectorateUpdated",            // gov-executed DAO message
		"bze.dao.EventDaoMembersUpdated",               // gov-executed DAO message
		"bze.dao.EventDaoMetadataUpdated",              // gov-executed DAO message
		"bze.dao.EventDepositReleased",                 // gov-executed DAO message
		"bze.dao.EventProposalCancelled",               // gov-executed DAO message
		"bze.dao.EventProposalCleaned",                 // gov-executed DAO message
		"bze.dao.EventProposalExecuted",                // gov-executed DAO message
		"bze.dao.EventProposalSettled",                 // gov-executed DAO message
		"bze.dao.EventProposalSubmitted",               // gov-executed DAO message
		"bze.dao.EventVote",                            // gov-executed DAO message
		"bze.epochs.EpochEndEvent",                     // BeginBlock
		"bze.epochs.EpochStartEvent",                   // BeginBlock
		"bze.guardian.v1.AutomationPausedEvent",        // gov-executed guardian message
		"bze.guardian.v1.AutomationResumedEvent",       // gov-executed guardian message
		"bze.guardian.v1.ChainHaltScheduledEvent",      // gov-executed guardian message
		"bze.guardian.v1.ChannelClosedEvent",           // gov-executed guardian message
		"bze.guardian.v1.ChannelReopenedEvent",         // gov-executed guardian message
		"bze.guardian.v1.DenomBlockedEvent",            // gov-executed guardian message
		"bze.guardian.v1.DenomUnblockedEvent",          // gov-executed guardian message
		"bze.guardian.v1.MessagesDisabledEvent",        // gov-executed guardian message
		"bze.guardian.v1.TightenRateLimitEvent",        // gov-executed guardian message
		"bze.guardian.v1.TradingDenomsHaltedEvent",     // gov-executed guardian message
		"bze.rewards.DenomRewardScheduleFinishEvent",   // daily distribution queue
		"bze.rewards.StakingRewardDistributionEvent",   // daily distribution queue
		"bze.rewards.TradingRewardDistributionEvent",   // hourly hook
		"bze.rewards.TradingRewardExpireEvent",         // hourly hook
		"bze.tokenfactory.DenomAdminChangeEvent",       // gov MsgSetDenomAdmin
		"bze.tradebin.LiquidityAddedEvent",             // periodic burn: locked coins into BZE liquidity
		"bze.tradebin.OrderCanceledEvent",              // EndBlock queue
		"bze.tradebin.OrderExecutedEvent",              // EndBlock queue
		"bze.tradebin.OrderMessageRefundedEvent",       // the v8.3.0 upgrade block
		"bze.tradebin.OrderSavedEvent",                 // EndBlock queue
		"bze.tradebin.PoolFeesUpdatedEvent",            // gov MsgUpdatePools
		"bze.tradebin.SwapEvent",                       // fee collector's conversion to BZE
		"bze.txfeecollector.CommunityPoolFeePaidEvent", // EndBlock community pool payout
	}, got)

	for _, txOnly := range []string{
		"bze.rewards.DenomRewardDistributionEvent", "bze.rewards.DenomRewardPrizeCreateEvent",
		"bze.rewards.StakingRewardFinishEvent", "bze.burner.FundBurnerEvent",
	} {
		_, ok := classify.LookupBlockEvent(txOnly)
		assert.False(t, ok, "%s is emitted by message handlers only", txOnly)
	}
}

// Two events of a design of chain v8.3.0 that no network ever emitted
// (removed before any release) are not classified.
func TestNeverShippedOrderEventsAreUnclassified(t *testing.T) {
	for _, typ := range []string{"bze.tradebin.OrderCancelSkippedEvent", "bze.tradebin.OrderMessageFailedEvent"} {
		_, ok := classify.LookupBlockEvent(typ)
		assert.False(t, ok, typ)
	}
}

// Chain v8.3.0 messages: the activity kind of every one, in governance
// unless it belongs to tokens; gov-only types stay "other".
func TestV830MessageKinds(t *testing.T) {
	kinds := map[string]string{
		"/bze.dao.MsgCreateDao":                 "dao_create",
		"/bze.dao.MsgSubmitProposal":            "dao_proposal",
		"/bze.dao.MsgVote":                      "dao_vote",
		"/bze.dao.MsgCancelProposal":            "dao_proposal",
		"/bze.dao.MsgSettleProposal":            "dao_proposal",
		"/bze.dao.MsgCleanupProposal":           "dao_proposal",
		"/bze.dao.MsgAdminExecute":              "dao_admin_execute",
		"/bze.dao.MsgNominateAdmin":             "dao_admin",
		"/bze.dao.MsgAcceptAdminNomination":     "dao_admin",
		"/bze.dao.MsgWithdrawAdminNomination":   "dao_admin",
		"/bze.dao.MsgUpdateDaoConfig":           "dao_update",
		"/bze.dao.MsgUpdateMetadata":            "dao_update",
		"/bze.dao.MsgUpdateElectorate":          "dao_update",
		"/bze.dao.MsgUpdateMembers":             "dao_update",
		"/bze.guardian.v1.MsgTightenRateLimit":  "guardian",
		"/bze.guardian.v1.MsgBlockDenom":        "guardian",
		"/bze.guardian.v1.MsgDisableMessages":   "guardian",
		"/bze.guardian.v1.MsgHaltTradingDenoms": "guardian",
		"/bze.guardian.v1.MsgHaltChain":         "guardian",
		"/bze.guardian.v1.MsgPauseAutomation":   "guardian",
		"/bze.guardian.v1.MsgCloseChannel":      "guardian",
		"/bze.tokenfactory.MsgDisableMinting":   "token_admin",
		"/bze.tokenfactory.MsgSetDenomConfig":   "token_update",
	}
	for url, kind := range kinds {
		m := classify.Lookup(url)
		assert.Equal(t, kind, m.Kind, url)
		want := classify.Governance
		if strings.HasPrefix(url, "/bze.tokenfactory.") {
			want = classify.Tokens
		}
		assert.Equal(t, want, m.SignerCategory, url)
		assert.Equal(t, want, m.ParticipantCategory, url)
	}
	for _, url := range []string{
		"/bze.dao.MsgUpdateParams", "/bze.guardian.v1.MsgUnblockDenom", "/bze.guardian.v1.MsgResumeAutomation",
		"/bze.guardian.v1.MsgReopenChannel", "/bze.guardian.v1.MsgUpdateParams", "/ratelimit.v1.MsgAddRateLimit",
		"/ratelimit.v1.MsgUpdateRateLimit", "/ratelimit.v1.MsgRemoveRateLimit", "/ratelimit.v1.MsgResetRateLimit",
		"/bze.tokenfactory.MsgSetDenomAdmin", "/bze.tradebin.MsgUpdatePools",
	} {
		assert.Equal(t, classify.KindOther, classify.Lookup(url).Kind, "%s is gov only", url)
	}
}

func TestV830BlockEvents(t *testing.T) {
	e, ok := classify.LookupBlockEvent("bze.txfeecollector.CommunityPoolFeePaidEvent")
	require.True(t, ok)
	assert.Equal(t, classify.BlockEventKind{
		EventType: "bze.txfeecollector.CommunityPoolFeePaidEvent", Kind: "community_pool_fee",
		Category: classify.Tokens, AddressAttrs: []string{"recipient"},
	}, e)
	for _, g := range []string{"TightenRateLimit", "DenomBlocked", "DenomUnblocked", "MessagesDisabled", "TradingDenomsHalted",
		"ChainHaltScheduled", "AutomationPaused", "AutomationResumed", "ChannelClosed", "ChannelReopened"} {
		e, ok := classify.LookupBlockEvent("bze.guardian.v1." + g + "Event")
		require.True(t, ok, g)
		assert.Equal(t, "guardian", e.Kind, g)
		assert.Equal(t, classify.Governance, e.Category, g)
		assert.Empty(t, e.AddressAttrs, g)
	}
	e, ok = classify.LookupBlockEvent("bze.tokenfactory.DenomAdminChangeEvent")
	require.True(t, ok)
	assert.Equal(t, "token_admin", e.Kind)
	assert.Equal(t, []string{"new_admin"}, e.AddressAttrs)
	e, ok = classify.LookupBlockEvent("bze.tradebin.PoolFeesUpdatedEvent")
	require.True(t, ok)
	assert.Equal(t, "dex_pool", e.Kind)
	assert.Equal(t, classify.DEX, e.Category)
	e, ok = classify.LookupBlockEvent("bze.dao.EventVote")
	require.True(t, ok)
	assert.Equal(t, []string{"voter"}, e.AddressAttrs)
	// Emitted inside transactions only.
	for _, tx := range []string{"bze.txfeecollector.GasSurchargePaidEvent", "bze.rewards.TradingRewardStartEvent"} {
		_, ok := classify.LookupBlockEvent(tx)
		assert.False(t, ok, tx)
	}
}
