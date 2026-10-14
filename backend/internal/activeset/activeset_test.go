package activeset_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/activeset"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

// pagedNode answers set in pages of size, whatever perPage asks.
type pagedNode struct {
	set   []node.Validator
	size  int
	total int // 0: len(set)
	err   error
	pages []int
}

func (n *pagedNode) Validators(_ context.Context, height int64, page, perPage int) (*node.Validators, []byte, error) {
	n.pages = append(n.pages, page)
	if perPage != node.ValidatorsPerPage {
		return nil, nil, fmt.Errorf("per_page %d", perPage)
	}
	if n.err != nil {
		return nil, nil, n.err
	}
	total := n.total
	if total == 0 {
		total = len(n.set)
	}
	lo := min((page-1)*n.size, len(n.set))
	hi := min(lo+n.size, len(n.set))
	return &node.Validators{Height: height, Validators: n.set[lo:hi], Total: total}, nil, nil
}

// mapResolver knows ops.
type mapResolver struct {
	ops   map[string]string
	err   error
	asked []string
}

func (r *mapResolver) Operators(_ context.Context, consensus []string) (map[string]string, error) {
	r.asked = consensus
	if r.err != nil {
		return nil, r.err
	}
	out := map[string]string{}
	for _, c := range consensus {
		if op, ok := r.ops[c]; ok {
			out[c] = op
		}
	}
	return out, nil
}

func val(addr string, power int64) node.Validator {
	return node.Validator{Address: addr, PubKey: node.PubKey{Type: "tendermint/PubKeyEd25519", Value: "k" + addr}, VotingPower: power}
}

func TestAtReadsEveryPageAndNamesTheOperators(t *testing.T) {
	n := &pagedNode{size: 2, set: []node.Validator{val("AA", 5), val("cc", 9), val("BB", 5), val("DD", 1), val("EE", 7)}}
	r := &mapResolver{ops: map[string]string{"CC": "bzevaloper1c", "AA": "bzevaloper1a", "EE": "bzevaloper1e"}}
	set, err := activeset.New(n, r).At(context.Background(), 42)
	require.NoError(t, err)

	assert.Equal(t, []int{1, 2, 3}, n.pages)
	assert.Equal(t, []string{"AA", "CC", "BB", "DD", "EE"}, r.asked, "consensus addresses upper-cased")
	assert.Equal(t, []activeset.Member{
		{ConsensusAddress: "CC", PubKey: "kcc", VotingPower: 9, Operator: "bzevaloper1c"},
		{ConsensusAddress: "EE", PubKey: "kEE", VotingPower: 7, Operator: "bzevaloper1e"},
		{ConsensusAddress: "AA", PubKey: "kAA", VotingPower: 5, Operator: "bzevaloper1a"},
		{ConsensusAddress: "BB", PubKey: "kBB", VotingPower: 5},
		{ConsensusAddress: "DD", PubKey: "kDD", VotingPower: 1},
	}, set, "by power descending, then address; an unknown operator stays empty")
}

func TestAtFailsOnAShortSet(t *testing.T) {
	n := &pagedNode{size: 2, total: 5, set: []node.Validator{val("AA", 5), val("BB", 4)}}
	_, err := activeset.New(n, &mapResolver{}).At(context.Background(), 42)
	require.ErrorContains(t, err, "page 2 is empty with 2 of 5 validators read")
}

func TestAtReturnsTheFailures(t *testing.T) {
	down := errors.New("node down")
	_, err := activeset.New(&pagedNode{err: down}, &mapResolver{}).At(context.Background(), 42)
	require.ErrorIs(t, err, down)

	dbErr := errors.New("db down")
	_, err = activeset.New(&pagedNode{size: 2, set: []node.Validator{val("AA", 1)}}, &mapResolver{err: dbErr}).At(context.Background(), 42)
	require.ErrorIs(t, err, dbErr)
}

func TestAtOverTheRecordedSet(t *testing.T) {
	n := fakenode.New(t)
	set, err := activeset.New(node.New(n.URL), &mapResolver{}).At(context.Background(), 23821226)
	require.NoError(t, err)
	require.Len(t, set, 23)
	var sum int64
	for _, m := range set {
		sum += m.VotingPower
		key, err := base64.StdEncoding.DecodeString(m.PubKey)
		require.NoError(t, err)
		assert.Equal(t, m.ConsensusAddress, activeset.ConsensusAddress(key), "the address is the key's")
	}
	assert.Equal(t, int64(131537480), sum)
	assert.Equal(t, "908785D2A4D25B062E669AAF65D3AB0727C92494", set[0].ConsensusAddress)
}

func TestRoutedReadsThePrunedHeightsFromTheArchive(t *testing.T) {
	local, archive := fakenode.New(t), fakenode.New(t)
	local.SetEarliestHeight(23821227)
	r := activeset.NewRouted(node.New(local.URL), node.New(archive.URL))

	set, err := activeset.New(r, &mapResolver{}).At(context.Background(), 23821226)
	require.NoError(t, err)
	assert.Len(t, set, 23)
	_, _, err = r.BlockResults(context.Background(), 23821225)
	require.NoError(t, err)
	assert.Equal(t, 1, local.RequestsAt(fakenode.RouteValidators, 23821226))
	assert.Equal(t, 1, archive.RequestsAt(fakenode.RouteValidators, 23821226))
	assert.Equal(t, 1, archive.RequestsAt(fakenode.RouteBlockResults, 23821225))

	// A height the local node has is read there only.
	local.SetEarliestHeight(0)
	_, err = activeset.New(r, &mapResolver{}).At(context.Background(), 17131439)
	require.NoError(t, err)
	assert.Equal(t, 1, local.RequestsAt(fakenode.RouteValidators, 17131439))
	assert.Zero(t, archive.RequestsAt(fakenode.RouteValidators, 17131439))

	// Without a local node every height goes to the archive.
	_, err = activeset.New(activeset.NewRouted(nil, node.New(archive.URL)), &mapResolver{}).At(context.Background(), 17131439)
	require.NoError(t, err)
	assert.Equal(t, 1, archive.RequestsAt(fakenode.RouteValidators, 17131439))
}
