package repository_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"

	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

func TestProposalReadsBindTheirFiltersAndKeysets(t *testing.T) {
	db := &fakeDB{queryErr: errDB}
	r := repository.NewExplorer(db)
	ctx := context.Background()
	status, option, before := "passed", "weighted", int64(40)

	_, err := r.Proposals(ctx, &status, &before, 21)
	assert.ErrorIs(t, err, errDB)
	assert.Contains(t, db.sql[0], "ORDER BY p.id DESC")
	assert.Equal(t, []any{&status, &before, 21}, db.args[0])

	_, err = r.ProposalVotes(ctx, 47, &option, true, &repository.VoteKey{Height: 9, TxIndex: 1, Voter: "bze1v"}, 21)
	assert.ErrorIs(t, err, errDB)
	assert.Contains(t, db.sql[1], "($2 = 'weighted' AND pv.option IS NULL)", "weighted means a split vote")
	assert.Contains(t, db.sql[1], "WHERE account_address = pv.voter", "a validator owner's vote names the validator")
	assert.Contains(t, db.sql[1], "(NOT $4::boolean OR v.operator_address IS NOT NULL)", "validators keeps the validator owners")
	assert.Contains(t, db.sql[1], "(pv.height, pv.tx_index, pv.voter) < ($5, $6, $7)")
	assert.Equal(t, []any{int64(47), &option, 21, true, int64(9), int64(1), "bze1v"}, db.args[1])

	_, err = r.ProposalDeposits(ctx, 47, nil, 21)
	assert.ErrorIs(t, err, errDB)
	assert.NotContains(t, db.sql[2], "<", "no keyset on the first page")
}

func TestProposalNotFound(t *testing.T) {
	db := &fakeDB{rowErr: pgx.ErrNoRows}
	_, err := repository.NewExplorer(db).Proposal(context.Background(), 9)
	assert.ErrorIs(t, err, repository.ErrNotFound)
	assert.Contains(t, db.sql[0], "v.status = 'bonded'", "validators voted counts the bonded set")
	assert.Contains(t, db.sql[0], "count(*) FROM explorer.proposal_votes pv WHERE pv.proposal_id = p.id", "the votes count")

	db = &fakeDB{rowErr: errDB}
	_, err = repository.NewExplorer(db).Proposal(context.Background(), 9)
	assert.ErrorIs(t, err, errDB)
}
