package snapshots

import (
	"context"

	gogoproto "github.com/cosmos/gogoproto/proto"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
)

// Node is the part of the RPC client the archive reader uses; *node.Client
// satisfies it.
type Node interface {
	Query(ctx context.Context, path string, req, resp gogoproto.Message, height int64) error
	Status(ctx context.Context) (*node.Status, []byte, error)
	Block(ctx context.Context, height int64) (*node.Block, []byte, error)
	BlockResults(ctx context.Context, height int64) (*node.BlockResults, []byte, error)
}

// Limiter paces the requests; *rate.Limiter satisfies it.
type Limiter interface {
	Wait(ctx context.Context) error
}

// Archive is the Reader over an archive node, every request paced by one
// limiter.
type Archive struct {
	node    Node
	limiter Limiter
}

var _ Reader = (*Archive)(nil)

// NewArchive returns the reader of n paced by limiter.
func NewArchive(n Node, limiter Limiter) *Archive {
	return &Archive{node: n, limiter: limiter}
}

// Query implements Reader.
func (a *Archive) Query(ctx context.Context, path string, req, resp gogoproto.Message, height int64) error {
	if err := a.limiter.Wait(ctx); err != nil {
		return err
	}
	return a.node.Query(ctx, path, req, resp, height)
}

// Status implements Reader.
func (a *Archive) Status(ctx context.Context) (*node.Status, error) {
	if err := a.limiter.Wait(ctx); err != nil {
		return nil, err
	}
	st, _, err := a.node.Status(ctx)
	return st, err
}

// Block implements Reader.
func (a *Archive) Block(ctx context.Context, height int64) (*node.Block, error) {
	if err := a.limiter.Wait(ctx); err != nil {
		return nil, err
	}
	b, _, err := a.node.Block(ctx, height)
	return b, err
}

// BlockResults implements Reader.
func (a *Archive) BlockResults(ctx context.Context, height int64) (*node.BlockResults, error) {
	if err := a.limiter.Wait(ctx); err != nil {
		return nil, err
	}
	r, _, err := a.node.BlockResults(ctx, height)
	return r, err
}
