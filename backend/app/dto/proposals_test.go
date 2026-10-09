package dto_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

func strp(s string) *string { return &s }

func TestTurnoutPct(t *testing.T) {
	tally := func(yes, no, abstain, veto string) *dto.Tally {
		return &dto.Tally{Yes: yes, No: no, Abstain: abstain, NoWithVeto: veto}
	}
	for _, tc := range []struct {
		name   string
		tally  *dto.Tally
		bonded *string
		want   *string
	}{
		{"every option counts", tally("60", "10", "5", "25"), strp("400"), strp("25.00000")},
		{"everyone voted", tally("300", "0", "0", "0"), strp("300"), strp("100.00000")},
		{"nobody voted", tally("0", "0", "0", "0"), strp("300"), strp("0.00000")},
		{"rounds half up", tally("1", "0", "0", "0"), strp("16000000"), strp("0.00001")}, // 0.00000625
		{"rounds down below half", tally("1", "0", "0", "0"), strp("30000000"), strp("0.00000")},
		{"thirds", tally("1", "0", "0", "0"), strp("3"), strp("33.33333")},
		// Mainnet proposal 47: the yes count over the bonded tokens recorded.
		{"mainnet sizes", tally("106584798486204", "0", "0", "0"), strp("147027371221650"), strp("72.49317")},
		{"no bonded tokens", tally("1", "0", "0", "0"), nil, nil},
		{"zero bonded tokens", tally("1", "0", "0", "0"), strp("0"), nil},
		{"not a number", tally("x", "0", "0", "0"), strp("10"), nil},
		{"no tally", nil, strp("10"), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := dto.TurnoutPct(tc.tally, tc.bonded)
			if tc.want == nil {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, *tc.want, *got)
		})
	}
}

func TestVoteAndDepositCursorsRoundTrip(t *testing.T) {
	v := repository.VoteKey{Height: 23745061, TxIndex: 0, Voter: "bze1k27v68x9gtppsgt9scr3649tpjfxnqlj04lk9x"}
	got, err := dto.DecodeVoteCursor(dto.EncodeVoteCursor(v))
	require.NoError(t, err)
	assert.Equal(t, &v, got)

	d := repository.DepositKey{Height: 9, TxIndex: 3, Depositor: "bze1p"}
	gotD, err := dto.DecodeDepositCursor(dto.EncodeDepositCursor(d))
	require.NoError(t, err)
	assert.Equal(t, &d, gotD)

	for _, bad := range []string{"!!", dto.EncodeCursor(1), "e30", dto.EncodeVoteCursor(repository.VoteKey{Height: -1, Voter: "x"})} {
		_, err := dto.DecodeVoteCursor(bad)
		assert.ErrorIs(t, err, dto.ErrInvalidCursor, bad)
	}
}

func TestNewProposalSummaryNeedsTheWholeTally(t *testing.T) {
	p := dto.NewProposalSummary(repository.ProposalSummary{ID: 1, TallyYes: strp("1")})
	assert.Nil(t, p.Tally)
	assert.Nil(t, p.TurnoutPct)
}
