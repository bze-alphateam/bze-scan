// Package serve runs the production process: the HTTP API and, when
// INDEXER_ENABLED is set, the live indexer, as components of one errgroup.
// It lives outside cmd/ so acceptance tests can run the real wiring
// in-process.
package serve

import (
	"context"
	"fmt"
	"net"

	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"

	"github.com/bze-alphateam/bze-scan/backend/app/server"
	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/indexer/live"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// Options are test hooks; the zero value is production.
type Options struct {
	// OnListen receives the HTTP server's bound address.
	OnListen func(net.Addr)
	// Live overrides the live indexer's tuning (retries, backoff, clock);
	// DatabaseURL is always taken from the configuration.
	Live live.Config
}

// component is one long-running part of the process. It runs until ctx is
// cancelled and returns nil on a clean stop; an error stops every other
// component.
type component struct {
	name string
	run  func(ctx context.Context) error
}

// Run runs every component until ctx is cancelled (by SIGINT/SIGTERM in
// production) or one of them fails. Later work registers the state sync, the
// status checker and the backfill here.
func Run(ctx context.Context, cfg *config.Config, opts Options) error {
	log.WithFields(log.Fields{
		"http_addr":       cfg.HTTPAddr,
		"log_level":       cfg.LogLevel,
		"indexer_enabled": cfg.IndexerEnabled,
	}).Info("starting bze-scan")

	e := server.New()
	components := []component{
		{name: "http", run: func(ctx context.Context) error {
			return server.Run(ctx, e, cfg.HTTPAddr, opts.OnListen)
		}},
	}

	if cfg.IndexerEnabled {
		if err := cfg.RequireDatabase(); err != nil {
			return fmt.Errorf("%w (or set INDEXER_ENABLED=false)", err)
		}
		nodeClient := node.New(cfg.NodeRPCURL)
		if err := checkChainID(ctx, nodeClient, cfg.ChainID); err != nil {
			return err
		}
		w, err := writer.NewLiveWriter(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		// Closed after every component has returned: the indexer finishes
		// the height in flight first.
		defer w.Close()

		lc := opts.Live
		lc.DatabaseURL = cfg.DatabaseURL
		ix := live.New(lc, nodeClient, w)
		components = append(components, component{name: "live indexer", run: ix.Run})
	} else {
		log.Info("live indexer disabled (INDEXER_ENABLED=false)")
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
