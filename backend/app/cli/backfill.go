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

func newBackfillCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "backfill",
		Short: "Run the history backfill standalone, down to BACKFILL_FLOOR",
		Long: "Indexes history from the archive node (ARCHIVE_RPC_URL), from the live " +
			"floor down to BACKFILL_FLOOR, with the configuration serve uses " +
			"(BACKFILL_ENABLED is ignored). It resumes from its checkpoint and exits " +
			"0 once the floor is reached, 1 otherwise. Only one backfill runs per " +
			"database: when another process holds it, the command exits 1 at once.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if err := cfg.RequireDatabase(); err != nil {
				return err
			}
			configureLogging(cfg)

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return serve.RunBackfill(ctx, cfg, serve.Options{})
		},
	}
}
