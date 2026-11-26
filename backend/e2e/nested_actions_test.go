//go:build e2e

package e2e_test

import (
	"context"
	"testing"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	daotypes "github.com/bze-alphateam/bze/x/dao/types"
	tokenfactorytypes "github.com/bze-alphateam/bze/x/tokenfactory/types"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// nestedProposalBlocks: proposal #9 of DAO #2 submitted at h with a denom
// metadata change and a validator description edit, executed by the settle
// at h+1 (whose body holds no messages).
func nestedProposalBlocks(t *testing.T, h int64) (submit, settle *transform.Entities) {
	t.Helper()
	denom := "factory/" + daoTwo + "/uhoney"
	var msgs []*codectypes.Any
	for _, m := range []sdk.Msg{
		&tokenfactorytypes.MsgSetDenomMetadata{Creator: daoTwo, Metadata: banktypes.Metadata{Base: denom, Symbol: "HNY", Name: "Honey", Display: "HNY"}},
		&stakingtypes.MsgEditValidator{ValidatorAddress: "bzevaloper1daonodedaonodedaonodedaonodedaonod", Description: stakingtypes.Description{
			Moniker: "DAO node", Identity: "[do-not-modify]", Website: "[do-not-modify]",
			SecurityContact: "[do-not-modify]", Details: "[do-not-modify]"}},
	} {
		a, err := codectypes.NewAnyWithValue(m)
		require.NoError(t, err)
		msgs = append(msgs, a)
	}
	submit = daoTxBlock(t, h,
		[]sdk.Msg{&daotypes.MsgSubmitProposal{Proposer: daoAdmin, DaoId: 2, Title: "Rename and brand", Msgs: msgs}},
		[][]node.Event{{daoSubmittedEvent("9", "2", "100", "1764666002", false)}})
	settle = daoTxBlock(t, h+1,
		[]sdk.Msg{&daotypes.MsgSettleProposal{Sender: daoMember, ProposalId: 9}},
		[][]node.Event{append([]node.Event{daoEvent("bze.tokenfactory.DenomMetadataChangeEvent", "0", "denom", denom)},
			daoSettledEvents("9", "EXECUTED", "60", "100", boolPtr(true), false)...)})
	return submit, settle
}

const nestedRowsSQL = `SELECT concat_ws(' ', 'token', height, tx_index, seq, kind, actor) FROM explorer.token_events
	UNION ALL
	SELECT concat_ws(' ', 'validator', height, tx_index, seq, kind, operator_address) FROM explorer.validator_events`

// The backfill writes the settle before the submission: the submission's
// flush writes the rows, with the settle's height and transaction. A second
// write and a reindex of both heights change nothing.
func TestSettledProposalActionsWhenTheSubmissionComesLater(t *testing.T) {
	e := newSyncEnv(t)
	pool, err := pgxpool.New(context.Background(), e.url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	const h = int64(25_300_000)
	submit, settle := nestedProposalBlocks(t, h)
	w := writer.NewBatchWriter(pool, writer.SettledProposalActions(nullLog()))

	require.NoError(t, w.Write(context.Background(), []*transform.Entities{settle}, writer.ModeInsert))
	assert.Empty(t, queryStrings(t, e.db, nestedRowsSQL), "no msgs yet: nothing to run")

	require.NoError(t, w.Write(context.Background(), []*transform.Entities{submit}, writer.ModeInsert))
	want := []string{
		"token 25300001 0 1000 metadata_changed " + daoTwo,
		"validator 25300001 0 1000 description_changed bzevaloper1daonodedaonodedaonodedaonodedaonod",
	}
	assert.Equal(t, want, queryStrings(t, e.db, nestedRowsSQL))

	require.NoError(t, w.Write(context.Background(), []*transform.Entities{settle, submit}, writer.ModeInsert))
	require.NoError(t, w.Write(context.Background(), []*transform.Entities{settle, submit}, writer.ModeUpdate))
	assert.Equal(t, want, queryStrings(t, e.db, nestedRowsSQL), "idempotent")
}

// The live path: the submission first, then the settle writes the rows.
func TestSettledProposalActionsOnTheLivePath(t *testing.T) {
	e := newSyncEnv(t)
	pool, err := pgxpool.New(context.Background(), e.url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	const h = int64(25_300_100)
	submit, settle := nestedProposalBlocks(t, h)
	w := writer.NewLiveWriter(pool, writer.SettledProposalActions(nullLog()))
	require.NoError(t, w.WriteBlock(context.Background(), submit))
	assert.Empty(t, queryStrings(t, e.db, nestedRowsSQL))
	require.NoError(t, w.WriteBlock(context.Background(), settle))
	assert.Equal(t, []string{
		"token 25300101 0 1000 metadata_changed " + daoTwo,
		"validator 25300101 0 1000 description_changed bzevaloper1daonodedaonodedaonodedaonodedaonod",
	}, queryStrings(t, e.db, nestedRowsSQL))
}

// Two proposals executed by one transaction (an authz exec of two settles
// leaves both under one message): each gets its own seq range, by id.
func TestTwoProposalsSettledInOneTransaction(t *testing.T) {
	e := newSyncEnv(t)
	pool, err := pgxpool.New(context.Background(), e.url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	const h = int64(25_300_200)
	denom := "factory/" + daoTwo + "/uhoney"
	brand, err := codectypes.NewAnyWithValue(&tokenfactorytypes.MsgSetDenomBranding{Creator: daoTwo, Denom: denom})
	require.NoError(t, err)
	submit := daoTxBlock(t, h,
		[]sdk.Msg{
			&daotypes.MsgSubmitProposal{Proposer: daoAdmin, DaoId: 2, Title: "Brand", Msgs: []*codectypes.Any{brand}},
			&daotypes.MsgSubmitProposal{Proposer: daoAdmin, DaoId: 2, Title: "Brand again", Msgs: []*codectypes.Any{brand}},
		},
		[][]node.Event{{daoSubmittedEvent("11", "2", "100", "1764666002", false)}, {daoSubmittedEvent("12", "2", "100", "1764666002", false)}})
	settle := daoTxBlock(t, h+1,
		[]sdk.Msg{&daotypes.MsgSettleProposal{Sender: daoMember, ProposalId: 11}},
		[][]node.Event{append(daoSettledEvents("11", "EXECUTED", "60", "100", boolPtr(true), false),
			daoSettledEvents("12", "EXECUTED", "60", "100", boolPtr(true), false)...)})
	w := writer.NewLiveWriter(pool, writer.SettledProposalActions(nullLog()))
	require.NoError(t, w.WriteBlock(context.Background(), submit))
	require.NoError(t, w.WriteBlock(context.Background(), settle))
	assert.Equal(t, []string{
		"token 25300201 0 1000 branding_changed " + daoTwo,
		"token 25300201 0 2000 branding_changed " + daoTwo,
	}, queryStrings(t, e.db, nestedRowsSQL))
}

func nullLog() logrus.FieldLogger {
	log, _ := logtest.NewNullLogger()
	return log
}
