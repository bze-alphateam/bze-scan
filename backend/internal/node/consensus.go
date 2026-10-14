package node

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// VoteType is the kind of a consensus vote.
type VoteType string

// The vote types of a round.
const (
	Prevote   VoteType = "prevote"
	Precommit VoteType = "precommit"
)

// ConsensusState is the part of /consensus_state the halt watch uses: the
// votes the node holds for the height it is deciding, every round.
type ConsensusState struct {
	// Height and Round are the node's position in consensus.
	Height int64
	Round  int32
	// Votes are the votes cast, every round; a validator that has not
	// voted in a round has no entry for it.
	Votes []RoundVote
	// Unparsed are the entries that are neither a vote nor "nil-Vote".
	Unparsed []string
}

// RoundVote is one vote of /consensus_state.
type RoundVote struct {
	// ValidatorIndex is the validator's position in the set of Height
	// (/validators?height=Height order).
	ValidatorIndex int
	// AddressPrefix is the first six bytes of its consensus address,
	// upper-case hex.
	AddressPrefix string
	Height        int64
	Round         int32
	Type          VoteType
	// Nil is a vote for no block.
	Nil bool
	// Timestamp is the validator's own time on the vote.
	Timestamp time.Time
}

// nilVote is how CometBFT prints a vote slot nobody filled.
const nilVote = "nil-Vote"

// voteRe matches a vote as CometBFT 0.38 prints it (types.Vote.String):
//
//	Vote{15:213C08735463 25084998/00/SIGNED_MSG_TYPE_PREVOTE(Prevote) 2F77010F43B2 D9165B062600 000000000000 @ 2026-10-14T12:06:54.698675395Z}
//
// index:address-fingerprint height/round/type(name) block-hash signature
// extension @ time; the block hash is empty for a nil vote.
var voteRe = regexp.MustCompile(`^Vote\{(\d+):([0-9A-F]*) (\d+)/(\d+)/\w+\((Prevote|Precommit)\) ([0-9A-F]*) [0-9A-F]* [0-9A-F]* @ (\S+)\}$`)

// ParseVote parses one vote string of /consensus_state; false for anything
// else ("nil-Vote" included).
func ParseVote(s string) (RoundVote, bool) {
	m := voteRe.FindStringSubmatch(s)
	if m == nil {
		return RoundVote{}, false
	}
	index, err1 := strconv.Atoi(m[1])
	height, err2 := strconv.ParseInt(m[3], 10, 64)
	round, err3 := strconv.ParseInt(m[4], 10, 32)
	ts, err4 := time.Parse(time.RFC3339Nano, m[7])
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		return RoundVote{}, false
	}
	return RoundVote{
		ValidatorIndex: index, AddressPrefix: m[2], Height: height, Round: int32(round),
		Type: VoteType(strings.ToLower(m[5])), Nil: m[6] == "", Timestamp: ts,
	}, true
}

// ConsensusState calls /consensus_state.
func (c *Client) ConsensusState(ctx context.Context) (*ConsensusState, []byte, error) {
	var res struct {
		RoundState struct {
			HeightRoundStep string `json:"height/round/step"`
			HeightVoteSet   []struct {
				Prevotes   []string `json:"prevotes"`
				Precommits []string `json:"precommits"`
			} `json:"height_vote_set"`
		} `json:"round_state"`
	}
	raw, err := c.call(ctx, "consensus_state", 0, &res)
	if err != nil {
		return nil, nil, err
	}
	hrs := strings.Split(res.RoundState.HeightRoundStep, "/")
	if len(hrs) != 3 {
		return nil, nil, fmt.Errorf("consensus_state: height/round/step is %q", res.RoundState.HeightRoundStep)
	}
	height, err := strconv.ParseInt(hrs[0], 10, 64)
	if err != nil {
		return nil, nil, fmt.Errorf("consensus_state: height %q: %w", hrs[0], err)
	}
	round, err := strconv.ParseInt(hrs[1], 10, 32)
	if err != nil {
		return nil, nil, fmt.Errorf("consensus_state: round %q: %w", hrs[1], err)
	}
	out := &ConsensusState{Height: height, Round: int32(round)}
	for _, rv := range res.RoundState.HeightVoteSet {
		for _, list := range [][]string{rv.Prevotes, rv.Precommits} {
			for _, s := range list {
				if s == nilVote {
					continue
				}
				if v, ok := ParseVote(s); ok {
					out.Votes = append(out.Votes, v)
				} else {
					out.Unparsed = append(out.Unparsed, s)
				}
			}
		}
	}
	return out, raw, nil
}
