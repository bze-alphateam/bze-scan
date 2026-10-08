package serve

import (
	"context"

	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/indexer/reindex"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
)

// NewReindex builds the reindex job over the backfill's pipeline and writer,
// with the local node (NODE_RPC_URL) for the heights it still has. workers,
// when positive, overrides BACKFILL_WORKERS. Call close when done.
func NewReindex(ctx context.Context, cfg *config.Config, opts Options, workers int) (job *reindex.Job, closeFn func(), err error) {
	if err := cfg.RequireDatabase(); err != nil {
		return nil, nil, err
	}
	codec, err := chain.NewCodec()
	if err != nil {
		return nil, nil, err
	}
	h, err := newHistory(ctx, cfg, opts, codec)
	if err != nil {
		return nil, nil, err
	}
	pcfg := h.cfg
	if workers > 0 {
		pcfg.Workers = workers
	}
	job = reindex.New(pcfg, reindex.Deps{
		Store:    reindex.NewPGStore(h.pool),
		Local:    node.New(cfg.NodeRPCURL),
		Pipeline: h.deps,
	})
	return job, h.pool.Close, nil
}
