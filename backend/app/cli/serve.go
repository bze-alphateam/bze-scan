package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/bze-alphateam/bze-scan/backend/app/serve"
	"github.com/bze-alphateam/bze-scan/backend/config"
)

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the explorer backend (HTTP API and live indexer)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			configureLogging(cfg)

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return serve.Run(ctx, cfg, serve.Options{})
		},
	}
}
