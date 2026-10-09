// Package serve runs the production process: the HTTP API over the explorer
// tables, the status checker and, when enabled, the live indexer (with the
// catch-up through the archive and the state sync it feeds) and the
// backfill, as components of one errgroup. It also runs the backfill standalone for the backfill command.
// It lives outside cmd/ so acceptance tests can run the real wiring
// in-process.
package serve

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"

	"github.com/bze-alphateam/bze-scan/backend/app/controller"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
	"github.com/bze-alphateam/bze-scan/backend/app/server"
	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/indexer/backfill"
	"github.com/bze-alphateam/bze-scan/backend/internal/indexer/live"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/rawcache"
	"github.com/bze-alphateam/bze-scan/backend/internal/status"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// Options are test hooks; the zero value is production.
type Options struct {
	// OnListen receives the HTTP server's bound address.
	OnListen func(net.Addr)
	// Live overrides the live indexer's tuning (retries, backoff, clock).
	Live live.Config
	// BackfillRetryDelays overrides the backfill's waits between retries.
	BackfillRetryDelays []time.Duration
}

// component is one long-running part of the process. It runs until ctx is
// cancelled and returns nil on a clean stop; an error stops every other
// component.
type component struct {
	name string
	run  func(ctx context.Context) error
}

// Run runs every component until ctx is cancelled (by SIGINT/SIGTERM in
// production) or one of them fails.
func Run(ctx context.Context, cfg *config.Config, opts Options) error {
	log.WithFields(log.Fields{
		"http_addr":        cfg.HTTPAddr,
		"log_level":        cfg.LogLevel,
		"indexer_enabled":  cfg.IndexerEnabled,
		"node_grpc_addr":   cfg.NodeGRPCAddr,
		"backfill_enabled": cfg.BackfillEnabled,
	}).Info("starting bze-scan")

	if err := cfg.RequireDatabase(); err != nil {
		return err
	}
	// The API's own pool; the live writer has another, so a burst of reads
	// never delays a block. Connections are opened on first use.
	apiPool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("API pool: %w", err)
	}
	defer apiPool.Close()

	// The status checker reads the explorer's height through the API pool
	// and the tips of both nodes, with or without the indexer. The raw-JSON
	// cache fetches its misses from the same archive nodes.
	archive := node.New(cfg.ArchiveRPCURL)
	// Nil interfaces when there is no retry node, never a typed nil.
	var statusRetry status.Node
	var rawRetry rawcache.Node
	if cfg.ArchiveRPCRetryURL != "" {
		retry := node.New(cfg.ArchiveRPCRetryURL)
		statusRetry, rawRetry = retry, retry
	}
	checker := status.New(status.Config{
		Interval:        cfg.StatusInterval,
		Tolerance:       cfg.StatusHeightTolerance,
		BackfillEnabled: cfg.BackfillEnabled,
	}, status.Deps{
		Store:        status.NewPGStore(apiPool),
		Local:        node.New(cfg.NodeRPCURL),
		Archive:      archive,
		ArchiveRetry: statusRetry,
	})
	raw := rawcache.New(rawcache.Config{
		MaxEntries: cfg.RawCacheMaxEntries,
		TTL:        cfg.RawCacheTTL,
	}, rawcache.Deps{
		Archive:      archive,
		ArchiveRetry: rawRetry,
	})

	// The chain codec decodes the node's gRPC answers and the indexed
	// transactions; built once, it is costly.
	var codec *chain.Codec
	if cfg.NodeGRPCAddr != "" || cfg.IndexerEnabled || cfg.BackfillEnabled {
		if codec, err = chain.NewCodec(); err != nil {
			return err
		}
	}
	// The account route reads balances and staking live through a
	// connection of its own; nil (no gRPC address) answers them unavailable.
	var accountState controller.AccountState
	if cfg.NodeGRPCAddr != "" {
		state, closeState, err := NewAccountState(cfg, codec)
		if err != nil {
			return err
		}
		defer closeState()
		accountState = state
	}

	explorerRepo := repository.NewExplorer(apiPool)
	e := server.New(server.Deps{
		Explorer:           explorerRepo,
		Accounts:           explorerRepo,
		Tokens:             explorerRepo,
		Proposals:          explorerRepo,
		AccountState:       accountState,
		Status:             checker,
		Raw:                raw,
		CORSAllowedOrigins: cfg.CORSAllowedOrigins,
		Log:                log.StandardLogger(),
	})
	components := []component{
		{name: "http", run: func(ctx context.Context) error {
			return server.Run(ctx, e, cfg.HTTPAddr, opts.OnListen)
		}},
		{name: "status checker", run: checker.Run},
	}

	// The live indexer's catch-up and the backfill share the history side:
	// its own pool, the batch writer and one archive rate limiter.
	var hist *history
	if cfg.IndexerEnabled || cfg.BackfillEnabled {
		if hist, err = newHistory(ctx, cfg, opts, codec); err != nil {
			return err
		}
		// Closed after every component has returned.
		defer hist.pool.Close()

		if cfg.IndexerEnabled {
			nodeClient := node.New(cfg.NodeRPCURL)
			if err := checkChainID(ctx, nodeClient, cfg.ChainID); err != nil {
				return err
			}
			pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
			if err != nil {
				return fmt.Errorf("live writer pool: %w", err)
			}
			// Closed after every component has returned: the indexer
			// finishes the height in flight first.
			defer pool.Close()

			syncer, closeSync, err := NewStateSync(ctx, cfg, codec)
			if err != nil {
				return err
			}
			defer closeSync()
			components = append(components, component{name: "state sync", run: syncer.Run})

			ix := live.New(opts.Live, live.Deps{
				Listener:    live.NewPGListener(cfg.DatabaseURL),
				Node:        nodeClient,
				Store:       writer.NewLiveWriter(pool),
				Transformer: transform.New(codec, log.StandardLogger()),
				Raw:         raw,
				CatchUp:     hist.catchUp(),
				Dirty:       syncer,
			})
			components = append(components, component{name: "live indexer", run: ix.Run})
		}
		if cfg.BackfillEnabled {
			components = append(components, backfillComponent(hist, cfg))
		}
	}
	if !cfg.IndexerEnabled {
		log.Info("live indexer disabled (INDEXER_ENABLED=false)")
	}
	if !cfg.BackfillEnabled {
		// An unfinished job reports paused until it is enabled again.
		if err := backfill.Pause(ctx, backfill.NewPGStore(apiPool)); err != nil {
			log.WithError(err).Warn("backfill: marking the checkpoint paused")
		}
	}

	g, gctx := errgroup.WithContext(ctx)
	for _, c := range components {
		g.Go(func() error {
			if err := c.run(gctx); err != nil {
				return fmt.Errorf("%s: %w", c.name, err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	log.Info("bze-scan stopped")
	return nil
}

// checkChainID refuses a node that serves another chain than CHAIN_ID.
func checkChainID(ctx context.Context, n *node.Client, want string) error {
	st, _, err := n.Status(ctx)
	if err != nil {
		return fmt.Errorf("node status: %w", err)
	}
	if st.Network != want {
		return fmt.Errorf("the node at NODE_RPC_URL serves chain %q, CHAIN_ID is %q", st.Network, want)
	}
	return nil
}
