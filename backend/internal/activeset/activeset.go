// Package activeset reads the active validator set at a height: the
// validators CometBFT lists for the block (/validators, every page), each
// with its operator address when the explorer knows it. CometBFT is the
// source; the set is never recomputed from staking state.
//
// The proposals' non-voters (the validators of the set a tally counted
// that did not vote) and the upgrade recovery read it.
package activeset

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/route"
)

// Member is one validator of the set.
type Member struct {
	// ConsensusAddress is upper-case hex, as /validators and /block report it.
	ConsensusAddress string
	// PubKey is the consensus public key, base64.
	PubKey string
	// VotingPower is the consensus power: the bonded tokens over 10^6,
	// truncated.
	VotingPower int64
	// Operator is the validator's operator address (bzevaloper1…), empty
	// when the explorer knows no validator with this consensus address.
	Operator string
}

// Reader reads the set at a height.
type Reader interface {
	// At returns the validators that sign the block at height, by voting
	// power descending, then consensus address.
	At(ctx context.Context, height int64) ([]Member, error)
}

// Node is the RPC the set is read from; *node.Client and *Routed satisfy it.
type Node interface {
	Validators(ctx context.Context, height int64, page, perPage int) (*node.Validators, []byte, error)
}

// Resolver names the operators of consensus addresses.
type Resolver interface {
	// Operators maps each consensus address (upper-case hex) it knows to
	// its operator address; the others are left out.
	Operators(ctx context.Context, consensus []string) (map[string]string, error)
}

// Set is the Reader over a node.
type Set struct {
	node     Node
	resolver Resolver
}

var _ Reader = (*Set)(nil)

// New returns the Reader over n, naming operators through r.
func New(n Node, r Resolver) *Set {
	return &Set{node: n, resolver: r}
}

// At reads every page of /validators at height and names the operators.
func (s *Set) At(ctx context.Context, height int64) ([]Member, error) {
	var out []Member
	for page := 1; ; page++ {
		res, _, err := s.node.Validators(ctx, height, page, node.ValidatorsPerPage)
		if err != nil {
			return nil, fmt.Errorf("active set at %d: %w", height, err)
		}
		for _, v := range res.Validators {
			out = append(out, Member{
				ConsensusAddress: strings.ToUpper(v.Address), PubKey: v.PubKey.Value, VotingPower: v.VotingPower,
			})
		}
		if len(out) >= res.Total {
			break
		}
		if len(res.Validators) == 0 {
			return nil, fmt.Errorf("active set at %d: page %d is empty with %d of %d validators read", height, page, len(out), res.Total)
		}
	}
	cons := make([]string, len(out))
	for i, m := range out {
		cons[i] = m.ConsensusAddress
	}
	ops, err := s.resolver.Operators(ctx, cons)
	if err != nil {
		return nil, fmt.Errorf("active set at %d: %w", height, err)
	}
	for i := range out {
		out[i].Operator = ops[out[i].ConsensusAddress]
	}
	slices.SortFunc(out, func(a, b Member) int {
		if a.VotingPower != b.VotingPower {
			if a.VotingPower > b.VotingPower {
				return -1
			}
			return 1
		}
		return strings.Compare(a.ConsensusAddress, b.ConsensusAddress)
	})
	return out, nil
}

// ConsensusAddress is the consensus address of an ed25519 consensus key:
// the first 20 bytes of its SHA-256, upper-case hex.
func ConsensusAddress(ed25519Key []byte) string {
	sum := sha256.Sum256(ed25519Key)
	return strings.ToUpper(hex.EncodeToString(sum[:20]))
}

// RoutedNode is the by-height RPC Routed reads; *node.Client satisfies it.
type RoutedNode interface {
	Node
	BlockResults(ctx context.Context, height int64) (*node.BlockResults, []byte, error)
	Commit(ctx context.Context, height int64) (*node.Commit, []byte, error)
}

// Routed reads every height from the local node, or from the archive when
// the local node pruned it (route.Via): a set minutes old is read locally, a
// set of years ago from the archive, through the same code.
type Routed struct {
	r route.Route[RoutedNode]
}

var _ RoutedNode = (*Routed)(nil)

// NewRouted returns a node over local and archive; a nil local reads every
// height from the archive.
func NewRouted(local, archive RoutedNode) *Routed {
	return &Routed{r: route.Route[RoutedNode]{Local: local, Remote: archive, HasLocal: local != nil}}
}

// Validators reads a page of /validators at height.
func (r *Routed) Validators(ctx context.Context, height int64, page, perPage int) (*node.Validators, []byte, error) {
	return route.Via(r.r, height, func(n RoutedNode) (*node.Validators, []byte, error) {
		return n.Validators(ctx, height, page, perPage)
	})
}

// BlockResults reads /block_results at height.
func (r *Routed) BlockResults(ctx context.Context, height int64) (*node.BlockResults, []byte, error) {
	return route.Via(r.r, height, func(n RoutedNode) (*node.BlockResults, []byte, error) {
		return n.BlockResults(ctx, height)
	})
}

// Commit reads /commit at height.
func (r *Routed) Commit(ctx context.Context, height int64) (*node.Commit, []byte, error) {
	return route.Via(r.r, height, func(n RoutedNode) (*node.Commit, []byte, error) {
		return n.Commit(ctx, height)
	})
}
