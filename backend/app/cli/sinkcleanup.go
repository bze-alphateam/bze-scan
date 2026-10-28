package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/bze-alphateam/bze-scan/backend/app/serve"
	"github.com/bze-alphateam/bze-scan/backend/config"
)

func newSinkCleanupCmd() *cobra.Command {
	var (
		retention time.Duration
		dryRun    bool
	)
	cmd := &cobra.Command{
		Use:   "sink-cleanup [--retention 48h] [--dry-run]",
		Short: "Delete the CometBFT psql sink's old heights once",
		Long: "One pass of the sink retention that serve runs every SINK_CLEANUP_INTERVAL: deletes " +
			"every height of the sink tables (public.blocks, tx_results, events, attributes) below " +
			"the cut-off, the lower of the newest block logged longer than the retention ago and the " +
			"live indexer's cursor minus 1000, in transactions of SINK_CLEANUP_BATCH heights. The " +
			"retention is --retention, else SINK_RETENTION; with neither the command refuses to run. " +
			"--dry-run prints the cut-off and the rows it would delete per table, and deletes nothing. " +
			"Never run it while another reader of the sink (the aggregator's listener) still needs " +
			"its rows. Exits 0 on success, 1 on a configuration or database error.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if retention < 0 {
				return fmt.Errorf("--retention must be positive (got %s)", retention)
			}
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			configureLogging(cfg)

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			cleaner, closeDB, err := serve.NewSinkCleaner(ctx, cfg, retention)
			if err != nil {
				return err
			}
			defer closeDB()

			plan, err := cleaner.Plan(ctx)
			if err != nil {
				return err
			}
			if dryRun {
				n, err := cleaner.Count(ctx, plan)
				if err != nil {
					return err
				}
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\nwould delete %s\n", plan, n)
				return err
			}
			n, err := cleaner.Clean(ctx, plan)
			if _, perr := fmt.Fprintf(cmd.OutOrStdout(), "%s\ndeleted %s\n", plan, n); perr != nil && err == nil {
				err = perr
			}
			return err
		},
	}
	f := cmd.Flags()
	f.DurationVar(&retention, "retention", 0, "keep the blocks logged within this long (default SINK_RETENTION)")
	f.BoolVar(&dryRun, "dry-run", false, "print the cut-off and the rows per table, delete nothing")
	return cmd
}
