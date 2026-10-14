package route_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/route"
)

// fakeNode answers its name, or err.
type fakeNode struct {
	name  string
	err   error
	calls int
}

func (n *fakeNode) read(h int64) (string, []byte, error) {
	n.calls++
	if n.err != nil {
		return "", nil, n.err
	}
	return fmt.Sprintf("%s@%d", n.name, h), nil, nil
}

func read(r route.Route[*fakeNode], h int64) (string, error) {
	v, _, err := route.Via(r, h, func(n *fakeNode) (string, []byte, error) { return n.read(h) })
	return v, err
}

func TestHeightsAtOrAboveEarliestGoToTheLocalNode(t *testing.T) {
	local, remote := &fakeNode{name: "local"}, &fakeNode{name: "remote"}
	r := route.Route[*fakeNode]{Local: local, Remote: remote, HasLocal: true, Earliest: 100}

	v, err := read(r, 100)
	require.NoError(t, err)
	assert.Equal(t, "local@100", v)
	v, err = read(r, 99)
	require.NoError(t, err)
	assert.Equal(t, "remote@99", v)
	assert.Equal(t, 1, local.calls, "a height below earliest never asks the local node")
}

func TestAHeightPrunedSinceGoesToTheRemoteNode(t *testing.T) {
	local := &fakeNode{err: fmt.Errorf("validators: %w", node.ErrPruned)}
	remote := &fakeNode{name: "remote"}
	v, err := read(route.Route[*fakeNode]{Local: local, Remote: remote, HasLocal: true}, 5)
	require.NoError(t, err)
	assert.Equal(t, "remote@5", v)
	assert.Equal(t, 1, local.calls)
}

func TestOtherLocalErrorsAreReturned(t *testing.T) {
	down := errors.New("connection refused")
	remote := &fakeNode{name: "remote"}
	_, err := read(route.Route[*fakeNode]{Local: &fakeNode{err: down}, Remote: remote, HasLocal: true}, 5)
	require.ErrorIs(t, err, down)
	assert.Zero(t, remote.calls)
}

func TestWithoutALocalNodeEveryHeightGoesToTheRemoteNode(t *testing.T) {
	v, err := read(route.Route[*fakeNode]{Remote: &fakeNode{name: "remote"}}, 1_000_000)
	require.NoError(t, err)
	assert.Equal(t, "remote@1000000", v)
}
