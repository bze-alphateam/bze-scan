package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/bze-alphateam/bze-scan/backend/app/serve"
	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
)

func newSyncStateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync-state",
		Short: "Resync every current-state table once from the local node's gRPC",
		Long: "Runs the full resync of every registered state-sync set (validators, " +
			"…) once, in order, against the local node's gRPC (NODE_GRPC_ADDR), " +
			"records each run in explorer.sync_jobs and exits 0 when every set " +
			"succeeded, 1 when any failed. serve does the same at every start and " +
			"keeps the tables fresh afterwards; this command is for operations.",
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
			codec, err := chain.NewCodec()
			if err != nil {
				return err
			}
			syncer, closeFn, err := serve.NewStateSync(ctx, cfg, codec)
			if err != nil {
				return err
			}
			defer closeFn()
			if err := syncer.RunOnce(ctx); err != nil {
				return fmt.Errorf("sync-state: %w", err)
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "sync-state done")
			return err
		},
	}
}
