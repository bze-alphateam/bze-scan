package controller_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/controller"
	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

func (f *fakeReader) limit(name string, n int) {
	if f.gotLimits == nil {
		f.gotLimits = map[string]int{}
	}
	f.gotLimits[name] = n
}

func (f *fakeReader) Validator(_ context.Context, operator string) (*repository.ValidatorDetail, error) {
	f.gotOperator = operator
	if f.err != nil {
		return nil, f.err
	}
	if f.validator == nil || f.validator.OperatorAddress != operator {
		return nil, repository.ErrNotFound
	}
	return f.validator, nil
}

func (f *fakeReader) ValidatorBlocks(_ context.Context, cons string, before *int64, limit int) ([]repository.BlockSummary, error) {
	f.gotCons, f.gotBefore = cons, before
	f.limit("blocks", limit)
	var out []repository.BlockSummary
	for _, b := range f.proposed {
		if (before == nil || b.Height < *before) && len(out) < limit {
			out = append(out, b)
		}
	}
	return out, f.err
}

func (f *fakeReader) ValidatorEvents(_ context.Context, operator string, limit int) ([]repository.ValidatorEvent, error) {
	f.limit("events", limit)
	return f.valEvents, f.err
}

func (f *fakeReader) VotesOf(_ context.Context, account string, limit int) ([]repository.ValidatorVote, error) {
	f.gotAccount = account
	f.limit("votes", limit)
	return f.votes, f.err
}

const consHex = "090703A2C594C5BA93C0D0E263A9F79AEEE17D10"

func validatorPage() *fakeReader {
	cons := consHex
	missed, window := int64(25), int64(10000)
	hash := hashUpper
	yes := "yes"
	return &fakeReader{
		validator: &repository.ValidatorDetail{
			ValidatorSummary: repository.ValidatorSummary{
				OperatorAddress: valoper, Moniker: "ChainTools", Tokens: "7895930982372", CommissionRate: "0.050000000000000000",
				MissedBlocks: &missed, SignedBlocksWindow: &window, Status: "bonded",
			},
			AccountAddress: account, ConsensusAddress: &cons, DelegatorShares: "7895930982372.000000000000000000",
			CommissionMaxRate: "0.500000000000000000", CommissionMaxChangeRate: "0.100000000000000000",
			UpdatedAt: time.Unix(1_800_000_000, 0),
		},
		proposed: blocksDown(500, 12),
		valEvents: []repository.ValidatorEvent{
			{Height: 400, TxIndex: -1, Kind: "slashed", Details: json.RawMessage(`{"reason":"missing_signature"}`), Time: time.Unix(1_800_000_000, 0)},
			{Height: 300, TxIndex: 2, Kind: "created", TxHash: &hash, Time: time.Unix(1_700_000_000, 0)},
		},
		votes: []repository.ValidatorVote{{ProposalID: 7, Option: &yes, Height: 350, Time: time.Unix(1_750_000_000, 0)}},
	}
}

func TestValidatorPage(t *testing.T) {
	f := validatorPage()
	rec := get(t, newAPI(f), "/validators/"+valoper)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl))

	v := decode[dto.ValidatorDetail](t, rec)
	assert.Equal(t, valoper, v.OperatorAddress)
	assert.Equal(t, account, v.AccountAddress)
	assert.Equal(t, "99.75000", *v.Uptime, "1 − missed / window")
	assert.Equal(t, int64(25), *v.MissedBlocks)

	require.Len(t, v.RecentBlocks, controller.ValidatorRecentBlocks)
	assert.Equal(t, int64(500), v.RecentBlocks[0].Height)
	require.Len(t, v.Events, 2)
	assert.Equal(t, "slashed", v.Events[0].Kind)
	assert.Nil(t, v.Events[0].TxHash, "a block-level event has no transaction")
	assert.JSONEq(t, `{"reason":"missing_signature"}`, string(v.Events[0].Details))
	assert.Equal(t, hashUpper, *v.Events[1].TxHash)
	assert.Equal(t, "null", string(v.Events[1].Details))
	require.Len(t, v.Votes, 1)
	assert.Equal(t, "[]", string(v.Votes[0].Options))

	assert.Equal(t, consHex, f.gotCons, "proposed blocks by consensus address")
	assert.Equal(t, account, f.gotAccount, "votes are cast by the owner account")
	assert.Equal(t, map[string]int{
		"blocks": controller.ValidatorRecentBlocks, "events": controller.ValidatorRecentEvents, "votes": controller.ValidatorRecentVotes,
	}, f.gotLimits)
}

func TestValidatorPageWithoutAConsensusAddressHasNoBlocks(t *testing.T) {
	f := validatorPage()
	f.validator.ConsensusAddress = nil
	rec := get(t, newAPI(f), "/validators/"+valoper)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, decode[dto.ValidatorDetail](t, rec).RecentBlocks)
	assert.NotContains(t, f.gotLimits, "blocks")
	assert.Contains(t, rec.Body.String(), `"recent_blocks":[]`)
}

func TestValidatorRoutesRejectAndMiss(t *testing.T) {
	e := newAPI(validatorPage())
	for _, path := range []string{"/validators/" + account, "/validators/nope", "/validators/" + account + "/blocks"} {
		rec := get(t, e, path)
		assert.Equal(t, http.StatusBadRequest, rec.Code, path)
		assert.Equal(t, "bad_request", errorCode(t, rec), path)
	}
	other := zeroAddress("bzevaloper")
	for _, path := range []string{"/validators/" + other, "/validators/" + other + "/blocks"} {
		rec := get(t, e, path)
		assert.Equal(t, http.StatusNotFound, rec.Code, path)
		assert.Equal(t, "not_found", errorCode(t, rec), path)
	}
}

func TestValidatorBlocksPaginates(t *testing.T) {
	f := validatorPage()
	e := newAPI(f)

	rec := get(t, e, "/validators/"+valoper+"/blocks?limit=5")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl))
	page := decode[dto.List[dto.BlockSummary]](t, rec)
	require.Len(t, page.Items, 5)
	assert.Equal(t, int64(496), page.Items[4].Height)
	require.NotNil(t, page.NextCursor)
	assert.Equal(t, 6, f.gotLimits["blocks"])

	rec = get(t, e, "/validators/"+valoper+"/blocks?limit=10&cursor="+*page.NextCursor)
	require.Equal(t, http.StatusOK, rec.Code)
	page = decode[dto.List[dto.BlockSummary]](t, rec)
	require.Len(t, page.Items, 7, "the 12 blocks end on this page")
	assert.Equal(t, int64(495), page.Items[0].Height)
	assert.Nil(t, page.NextCursor)

	for _, q := range []string{"?limit=0", "?cursor=x"} {
		assert.Equal(t, http.StatusBadRequest, get(t, e, "/validators/"+valoper+"/blocks"+q).Code, q)
	}

	f.validator.ConsensusAddress = nil
	rec = get(t, e, "/validators/"+valoper+"/blocks")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"items":[],"next_cursor":null}`, rec.Body.String())
}

func TestBlockNamesItsProposer(t *testing.T) {
	cons := consHex
	f := &fakeReader{block: &repository.Block{BlockSummary: repository.BlockSummary{
		Height: 42, Time: time.Unix(1_700_000_000, 0), Hash: "H", ProposerConsAddress: &cons,
		Proposer: &repository.Proposer{OperatorAddress: valoper, Moniker: "ChainTools"},
	}}}
	e := newAPI(f)
	rec := get(t, e, "/blocks/42")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, controller.CacheImmutable, rec.Header().Get(echo.HeaderCacheControl))
	assert.Equal(t, &dto.Proposer{OperatorAddress: valoper, Moniker: "ChainTools"}, decode[dto.Block](t, rec).Proposer)

	// Unknown until the state sync knows the validator: not cached.
	f.block.Proposer = nil
	rec = get(t, e, "/blocks/42")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, controller.CacheNoStore, rec.Header().Get(echo.HeaderCacheControl))
	assert.Contains(t, rec.Body.String(), `"proposer":null`)

	f.blocks = []repository.BlockSummary{f.block.BlockSummary}
	f.blocks[0].Proposer = &repository.Proposer{OperatorAddress: valoper, Moniker: "ChainTools"}
	page := decode[dto.List[dto.BlockSummary]](t, get(t, e, "/blocks"))
	require.Len(t, page.Items, 1)
	assert.Equal(t, "ChainTools", page.Items[0].Proposer.Moniker)
}
