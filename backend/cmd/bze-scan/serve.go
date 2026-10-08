package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/bze-alphateam/bze-scan/backend/app/server"
	"github.com/bze-alphateam/bze-scan/backend/config"
)

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the explorer backend (HTTP API)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			configureLogging(cfg)

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return runServe(ctx, cfg)
		},
	}
}

// component is one long-running part of the serve process. It runs until ctx
// is cancelled and returns nil on a clean stop; an error stops every other
// component.
type component struct {
	name string
	run  func(ctx context.Context) error
}

// runServe runs every component of the process until ctx is cancelled (by
// SIGINT/SIGTERM in production) or one of them fails. Later work registers the
// live indexer, the state sync, the status checker and the backfill here,
// next to the HTTP server.
func runServe(ctx context.Context, cfg *config.Config) error {
	log.WithFields(log.Fields{
		"http_addr": cfg.HTTPAddr,
		"log_level": cfg.LogLevel,
	}).Info("starting bze-scan")

	e := server.New()
	components := []component{
		{name: "http", run: func(ctx context.Context) error {
			return server.Run(ctx, e, cfg.HTTPAddr, nil)
		}},
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
