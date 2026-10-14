// Package route sends a by-height read to the local node while it still has
// the height, else to the archive: the routing the reindex command and the
// active set at a height share. The local node is pruned, so it answers
// recent heights faster than the archive and the archive answers the rest.
package route

import (
	"errors"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
)

// Route picks the node a height is read from.
type Route[N any] struct {
	Local  N
	Remote N
	// HasLocal is false without a local node: every height goes to Remote.
	HasLocal bool
	// Earliest is the lowest height Local still has (its /status
	// earliest_block_height). Heights below it go to Remote without asking
	// Local; 0 asks Local for every height.
	Earliest int64
}

// Via calls call on the node r picks for height h. A height Local pruned
// since Earliest was read goes to Remote too.
func Via[N, T any](r Route[N], h int64, call func(N) (T, []byte, error)) (T, []byte, error) {
	if !r.HasLocal || h < r.Earliest {
		return call(r.Remote)
	}
	v, raw, err := call(r.Local)
	if errors.Is(err, node.ErrPruned) {
		return call(r.Remote)
	}
	return v, raw, err
}
