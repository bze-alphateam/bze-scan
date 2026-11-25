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

// pendingBFE348 are messages the chain registers (v8.3.0) that BFE-348
// classifies; until then they are accepted unclassified.
// TODO(BFE-348): classify them and delete this list.
var pendingBFE348 = []string{
	"/bze.dao.MsgAcceptAdminNomination",
	"/bze.dao.MsgAdminExecute",
	"/bze.dao.MsgCancelProposal",
	"/bze.dao.MsgCleanupProposal",
	"/bze.dao.MsgCreateDao",
	"/bze.dao.MsgNominateAdmin",
	"/bze.dao.MsgSettleProposal",
	"/bze.dao.MsgSubmitProposal",
	"/bze.dao.MsgUpdateDaoConfig",
	"/bze.dao.MsgUpdateElectorate",
	"/bze.dao.MsgUpdateMembers",
	"/bze.dao.MsgUpdateMetadata",
	"/bze.dao.MsgUpdateParams",
	"/bze.dao.MsgVote",
	"/bze.dao.MsgWithdrawAdminNomination",
	"/bze.guardian.v1.MsgBlockDenom",
	"/bze.guardian.v1.MsgCloseChannel",
	"/bze.guardian.v1.MsgDisableMessages",
	"/bze.guardian.v1.MsgHaltChain",
	"/bze.guardian.v1.MsgHaltTradingDenoms",
	"/bze.guardian.v1.MsgPauseAutomation",
	"/bze.guardian.v1.MsgReopenChannel",
	"/bze.guardian.v1.MsgResumeAutomation",
	"/bze.guardian.v1.MsgTightenRateLimit",
	"/bze.guardian.v1.MsgUnblockDenom",
	"/bze.guardian.v1.MsgUpdateParams",
	"/bze.tokenfactory.MsgDisableMinting",
	"/bze.tokenfactory.MsgSetDenomAdmin",
	"/bze.tokenfactory.MsgSetDenomConfig",
	"/bze.tradebin.MsgUpdatePools",
	"/ratelimit.v1.MsgAddRateLimit",
	"/ratelimit.v1.MsgRemoveRateLimit",
	"/ratelimit.v1.MsgResetRateLimit",
	"/ratelimit.v1.MsgUpdateRateLimit",
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
	pending := map[string]bool{}
	for _, u := range pendingBFE348 {
		pending[u] = true
		assert.True(t, registered[u], "%s is pending BFE-348 but not registered by the chain", u)
	}

	classified := map[string]bool{}
	for _, m := range classify.Messages() {
		classified[m.TypeURL] = true
		assert.True(t, registered[m.TypeURL] || retired[m.TypeURL], "%s is classified but not registered by the chain", m.TypeURL)
		assert.False(t, pending[m.TypeURL], "%s is classified: drop it from pendingBFE348", m.TypeURL)
	}
	allowed := map[string]bool{}
	for _, u := range classify.Unclassified {
		allowed[u] = true
		assert.True(t, registered[u] || retired[u], "%s is allow-listed but not registered by the chain", u)
		assert.False(t, classified[u], "%s is both classified and allow-listed", u)
	}
	for u := range registered {
		assert.True(t, classified[u] || allowed[u] || pending[u], "%s has no classification entry and is not allow-listed", u)
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
		"bze.burner.CoinsBurnedEvent",                // periodic burn, raffle clean-up
		"bze.burner.RaffleFinishedEvent",             // raffle clean-up hook
		"bze.burner.RaffleLostEvent",                 // EndBlock draw
		"bze.burner.RaffleWinnerEvent",               // EndBlock draw
		"bze.epochs.EpochEndEvent",                   // BeginBlock
		"bze.epochs.EpochStartEvent",                 // BeginBlock
		"bze.rewards.DenomRewardScheduleFinishEvent", // daily distribution queue
		"bze.rewards.StakingRewardDistributionEvent", // daily distribution queue
		"bze.rewards.TradingRewardDistributionEvent", // hourly hook
		"bze.rewards.TradingRewardExpireEvent",       // hourly hook
		"bze.tradebin.LiquidityAddedEvent",           // periodic burn: locked coins into BZE liquidity
		"bze.tradebin.OrderCanceledEvent",            // EndBlock queue
		"bze.tradebin.OrderExecutedEvent",            // EndBlock queue
		"bze.tradebin.OrderMessageRefundedEvent",     // the v8.3.0 upgrade block
		"bze.tradebin.OrderSavedEvent",               // EndBlock queue
		"bze.tradebin.SwapEvent",                     // fee collector's conversion to BZE
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
