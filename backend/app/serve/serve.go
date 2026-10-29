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
	"github.com/bze-alphateam/bze-scan/backend/internal/candles"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/debugserver"
	"github.com/bze-alphateam/bze-scan/backend/internal/dexunits"
	"github.com/bze-alphateam/bze-scan/backend/internal/indexer/backfill"
	"github.com/bze-alphateam/bze-scan/backend/internal/indexer/live"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/ordercoverage"
	"github.com/bze-alphateam/bze-scan/backend/internal/rawcache"
	"github.com/bze-alphateam/bze-scan/backend/internal/stakeflow"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/upgrades"
	"github.com/bze-alphateam/bze-scan/backend/internal/status"
	"github.com/bze-alphateam/bze-scan/backend/internal/swaphistory"
	"github.com/bze-alphateam/bze-scan/backend/internal/trades"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// Options are test hooks; the zero value is production.
type Options struct {
	// OnListen receives the HTTP server's bound address.
	OnListen func(net.Addr)
	// OnDebugListen receives the debug server's bound address (DEBUG_ADDR).
	OnDebugListen func(net.Addr)
	// Live overrides the live indexer's tuning (retries, backoff, clock).
	Live live.Config
	// BackfillRetryDelays overrides the backfill's waits between retries.
	BackfillRetryDelays []time.Duration
	// BackfillUnavailableMin overrides the backfill's first wait while the
	// archive is unreachable.
	BackfillUnavailableMin time.Duration
	// BackfillLockRetry overrides how often serve tries again for the
	// backfill's lock while another process holds it.
	BackfillLockRetry time.Duration
	// OrderCoverage overrides the open orders' coverage checker (DEX start
	// heights, interval); ChainID is always CHAIN_ID.
	OrderCoverage ordercoverage.Config
	// TradesInterval overrides how often the stored trades are numbered
	// again (trades.Interval).
	TradesInterval time.Duration
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
		"halt_watch":       cfg.IndexerEnabled && cfg.HaltWatchEnabled,
		"debug_addr":       cfg.DebugAddr,
		"sink_retention":   cfg.SinkRetention,
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
	// The parameters routes (the halted denoms too) read them live; without
	// a gRPC address they are left out.
	var liveParams controller.LiveParams
	var haltedDenoms controller.HaltedDenomsReader
	if cfg.NodeGRPCAddr != "" {
		lp, closeParams, err := NewLiveParams(cfg, codec)
		if err != nil {
			return err
		}
		defer closeParams()
		liveParams, haltedDenoms = lp, lp
	}

	// The open orders route reads the orders table when its history is
	// complete (checked by a component of its own) and the nodes otherwise.
	var openOrders controller.OpenOrdersReader
	var coverage component
	if cfg.NodeGRPCAddr != "" {
		reader, cov, closeOpenOrders, err := NewOpenOrders(cfg, codec, apiPool, opts.OrderCoverage)
		if err != nil {
			return err
		}
		defer closeOpenOrders()
		openOrders, coverage = reader, component{name: "order coverage", run: cov.Run}
	}

	// The validators' stake movements of the last 7 days live in memory:
	// loaded once by a component of their own, then fed by both writers.
	flows := stakeflow.New(stakeflow.Config{}, stakeflow.Deps{Store: stakeflow.NewPGStore(apiPool)})

	// The halt watch runs with the live indexer; the API reads its latest
	// poll. A nil interface without it, never a typed nil.
	var watch *upgrades.Watch
	var liveHalt controller.LiveHaltReader
	if cfg.IndexerEnabled && cfg.HaltWatchEnabled {
		w, closeWatch, err := NewHaltWatch(ctx, cfg)
		if err != nil {
			return err
		}
		defer closeWatch()
		watch, liveHalt = w, w
	}

	// The aggregator-compatible /aggregator/api/prices; a nil interface without a
	// CoinGecko host, never a typed nil.
	var compatPrices controller.PriceFeed
	if feed := NewPriceFeed(cfg); feed != nil {
		compatPrices = feed
	}
	var compatArticles controller.ArticleFeed
	if feed := NewArticleFeed(cfg); feed != nil {
		compatArticles = feed
	}

	// The aggregator-compatible /aggregator/api/dex/orders reads the books live, and
	// /aggregator/api/dex/intervals the markets' denoms; left out without a gRPC address.
	var compatOrders controller.OrderBooks
	var compatIntervals controller.Intervals
	if cfg.NodeGRPCAddr != "" {
		books, closeBooks, err := NewOrderBook(cfg, codec, apiPool)
		if err != nil {
			return err
		}
		defer closeBooks()
		compatOrders = books
		compatIntervals = candles.NewService(candles.ServiceDeps{Reader: candles.NewPGReader(apiPool), Markets: books,
			Exponents: dexunits.NewPGStore(apiPool)})
	}

	explorerRepo := repository.NewExplorer(apiPool)

	// The aggregator-compatible /aggregator/api/supply routes read the local node.
	var compatSupply controller.SupplyReader
	if cfg.NodeGRPCAddr != "" {
		sup, closeSupply, err := NewSupply(cfg, codec, explorerRepo)
		if err != nil {
			return err
		}
		defer closeSupply()
		compatSupply = sup
	}

	e := server.New(server.Deps{
		Explorer:        explorerRepo,
		Accounts:        explorerRepo,
		Tokens:          explorerRepo,
		Proposals:       explorerRepo,
		Upgrades:        explorerRepo,
		Stats:           explorerRepo,
		Daily:           explorerRepo,
		Params:          liveParams,
		ParamChanges:    explorerRepo,
		HaltedDenoms:    haltedDenoms,
		AccountState:    accountState,
		OpenOrders:      openOrders,
		StakeFlows:      flows,
		ValidatorNames:  explorerRepo,
		LiveHalt:        liveHalt,
		Status:          checker,
		Raw:             raw,
		CompatPrices:    compatPrices,
		CompatArticles:  compatArticles,
		CompatOrders:    compatOrders,
		CoinGeckoMerges: cfg.CoinGeckoMergedMarkets,
		CompatSupply:    compatSupply,
		CompatIntervals: compatIntervals,
		CompatSwaps: swaphistory.New(swaphistory.Deps{Store: swaphistory.NewPGStore(apiPool),
			Exponents: dexunits.NewPGStore(apiPool), Log: log.StandardLogger()}),
		CORSAllowedOrigins: cfg.CORSAllowedOrigins,
		Log:                log.StandardLogger(),
	})
	components := []component{
		{name: "http", run: func(ctx context.Context) error {
			return server.Run(ctx, e, cfg.HTTPAddr, opts.OnListen)
		}},
		{name: "status checker", run: checker.Run},
		{name: "stake movements", run: flows.Run},
	}
	if coverage.run != nil {
		components = append(components, coverage)
	}
	// The sink cleanup runs only with SINK_RETENTION set (off by default).
	if cfg.SinkRetention > 0 {
		components = append(components, component{name: "sink cleanup", run: newSinkCleaner(cfg, 0, apiPool).Run})
	}
	if cfg.DebugAddr != "" {
		components = append(components, component{name: "debug server", run: func(ctx context.Context) error {
			return debugserver.Run(ctx, cfg.DebugAddr, opts.OnDebugListen)
		}})
	}

	// The live indexer's catch-up and the backfill share the history side:
	// its own pool, the batch writer and one archive rate limiter.
	var hist *history
	if cfg.IndexerEnabled || cfg.BackfillEnabled {
		if hist, err = newHistory(ctx, cfg, opts, codec, flows.PostFlush); err != nil {
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
			if watch != nil {
				components = append(components, component{name: "halt watch", run: watch.Run})
			}

			ix := live.New(opts.Live, live.Deps{
				Listener:    live.NewPGListener(cfg.DatabaseURL),
				Node:        nodeClient,
				Store:       writer.NewLiveWriter(pool, writer.AttributeOrders, candles.Default.PostFlush, flows.PostFlush),
				Transformer: transform.New(codec, log.StandardLogger()),
				Raw:         raw,
				CatchUp:     hist.catchUp(),
				Dirty:       syncer,
			})
			components = append(components, component{name: "live indexer", run: ix.Run})
			// The blocks a previous release's indexer writes while both run
			// (a deploy's overlap) carry no trade ids: numbered from the
			// stored rows at start and every minute.
			components = append(components, component{name: "trades", run: func(ctx context.Context) error {
				return trades.Run(ctx, pool, opts.TradesInterval, log.StandardLogger())
			}})
		}
		if cfg.BackfillEnabled {
			components = append(components, backfillComponent(hist, cfg, opts.BackfillLockRetry))
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
