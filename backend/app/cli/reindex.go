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
	"github.com/bze-alphateam/bze-scan/backend/internal/indexer/reindex"
)

// ExitSomeFailed is the reindex's exit code when some heights failed.
const ExitSomeFailed = 2

func newReindexCmd() *cobra.Command {
	var (
		flags    reindex.Flags
		from, to int64
		workers  int
		dryRun   bool
	)
	cmd := &cobra.Command{
		Use:   "reindex (--heights H,H,… | --from N --to M | --failed [--source S])",
		Short: "Run heights through the backfill pipeline again, overwriting what is there",
		Long: "Re-indexes the selected heights and overwrites their rows: the repair tool after an " +
			"outage, for the heights listed in index_failures, or after a transformer fix. Each " +
			"height is read from the local node (NODE_RPC_URL) when it still has it, else from the " +
			"archive (ARCHIVE_RPC_URL, ARCHIVE_RPC_RETRY_URL on error), with the backfill's " +
			"configuration. Exits 0 when every height succeeded, 2 when some failed (listed on " +
			"stderr and recorded in index_failures), 1 on a configuration or database error.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("from") {
				flags.From = &from
			}
			if cmd.Flags().Changed("to") {
				flags.To = &to
			}
			sel, err := reindex.Parse(flags)
			if err != nil {
				return err
			}
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
			job, closeDB, err := serve.NewReindex(ctx, cfg, serve.Options{}, workers)
			if err != nil {
				return err
			}
			defer closeDB()

			plan, err := job.Plan(ctx, sel)
			if err != nil {
				return err
			}
			if dryRun {
				_, err := fmt.Fprint(cmd.OutOrStdout(), plan.Describe())
				return err
			}
			res, err := job.Run(ctx, plan)
			if err != nil {
				return err
			}
			for _, h := range res.Failed {
				if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "failed height %d\n", h); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), res.Summary()); err != nil {
				return err
			}
			if len(res.Failed) > 0 {
				return &ExitError{Code: ExitSomeFailed, Err: fmt.Errorf("%d of %d heights failed", len(res.Failed), res.Heights)}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&flags.Heights, "heights", "", "comma-separated heights")
	f.Int64Var(&from, "from", 0, "lowest height of a range (with --to)")
	f.Int64Var(&to, "to", 0, "highest height of a range (with --from), dispatched downward")
	f.BoolVar(&flags.Failed, "failed", false, "every height of index_failures not resolved yet")
	f.StringVar(&flags.Source, "source", "", "with --failed: only the failures of live, backfill or reindex")
	f.BoolVar(&dryRun, "dry-run", false, "print the selection and exit")
	f.IntVar(&workers, "workers", 0, "heights fetched in parallel (default BACKFILL_WORKERS)")
	return cmd
}
