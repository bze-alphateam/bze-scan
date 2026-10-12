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
	"github.com/bze-alphateam/bze-scan/backend/internal/snapshots"
)

func newSnapshotsCmd() *cobra.Command {
	var (
		sel     snapshots.Selection
		workers int
	)
	cmd := &cobra.Command{
		Use:   "snapshots [--kind proposals] [--id N]...",
		Short: "Read past state from the archive node and write it over the database's",
		Long: "Reads values the chain no longer has, or that no event carries, from the archive " +
			"node (ARCHIVE_RPC_URL, paced by BACKFILL_RATE_LIMIT) at the height they describe, and " +
			"writes them over what the database holds. It never deletes a row; running it again " +
			"writes the same values, so after a fix one run repairs history. Kinds: proposals (the " +
			"bonded tokens, votes and deposits of every resolved proposal, read just before its " +
			"tally). By default every kind and every target; --kind and --id narrow it. Run by hand " +
			"only, never by serve. Exits 0 when every target succeeded, 2 when some failed (listed " +
			"on stderr and in sync_jobs as snapshot:<kind>), 1 on a configuration or database error.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if workers < 0 {
				return fmt.Errorf("--workers must be positive (got %d)", workers)
			}
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			configureLogging(cfg)

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			runner, closeDB, err := serve.NewSnapshots(ctx, cfg, workers)
			if err != nil {
				return err
			}
			defer closeDB()

			res, err := runner.Run(ctx, sel)
			if err != nil {
				return err
			}
			for _, f := range res.Failed {
				if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "failed %s\n", f); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), res.Summary()); err != nil {
				return err
			}
			if len(res.Failed) > 0 {
				return &ExitError{Code: ExitSomeFailed, Err: fmt.Errorf("%d snapshots failed", len(res.Failed))}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringSliceVar(&sel.Kinds, "kind", nil, "only this kind (repeatable): proposals")
	f.StringSliceVar(&sel.IDs, "id", nil, "only this target of the kind, e.g. a proposal id (repeatable)")
	f.IntVar(&workers, "workers", 0, fmt.Sprintf("targets snapshotted in parallel (default %d)", snapshots.DefaultWorkers))
	return cmd
}
