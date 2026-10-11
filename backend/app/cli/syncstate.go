package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/bze-alphateam/bze-scan/backend/app/serve"
	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/dailystats"
)

// syncStateFlags are the options of sync-state.
type syncStateFlags struct {
	job, from, to string
}

// dayRange parses --from and --to; ok is false when neither is set.
func (f syncStateFlags) dayRange() (from, to time.Time, ok bool, err error) {
	if f.from == "" && f.to == "" {
		return time.Time{}, time.Time{}, false, nil
	}
	if f.job != statesync.DailyStats {
		return time.Time{}, time.Time{}, false, fmt.Errorf("--from and --to need --job %s", statesync.DailyStats)
	}
	if f.from == "" || f.to == "" {
		return time.Time{}, time.Time{}, false, errors.New("--from and --to go together")
	}
	if from, err = dailystats.ParseDay(f.from); err != nil {
		return time.Time{}, time.Time{}, false, fmt.Errorf("--from: %w", err)
	}
	if to, err = dailystats.ParseDay(f.to); err != nil {
		return time.Time{}, time.Time{}, false, fmt.Errorf("--to: %w", err)
	}
	return from, to, true, nil
}

func newSyncStateCmd() *cobra.Command {
	var f syncStateFlags
	cmd := &cobra.Command{
		Use:   "sync-state",
		Short: "Resync every current-state table once from the local node's gRPC",
		Long: "Runs the full resync of every registered state-sync set (validators, " +
			"…) once, in order, against the local node's gRPC (NODE_GRPC_ADDR), " +
			"records each run in explorer.sync_jobs and exits 0 when every set " +
			"succeeded, 1 when any failed. serve does the same at every start and " +
			"keeps the tables fresh afterwards; this command is for operations.\n\n" +
			"--job runs one set only. --job daily_stats --from YYYY-MM-DD --to YYYY-MM-DD " +
			"recomputes those closed UTC days from the indexed tables (after a reindex, " +
			"for example), with the database alone.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			from, to, isRange, err := f.dayRange()
			if err != nil {
				return err
			}
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
			if isRange {
				return recomputeDays(ctx, cmd, cfg, from, to)
			}
			codec, err := chain.NewCodec()
			if err != nil {
				return err
			}
			syncer, closeFn, err := serve.NewStateSync(ctx, cfg, codec)
			if err != nil {
				return err
			}
			defer closeFn()
			if f.job != "" {
				err = syncer.RunSet(ctx, f.job)
			} else {
				err = syncer.RunOnce(ctx)
			}
			if err != nil {
				return fmt.Errorf("sync-state: %w", err)
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "sync-state done")
			return err
		},
	}
	cmd.Flags().StringVar(&f.job, "job", "", "run only this set (validators, daily_stats, …)")
	cmd.Flags().StringVar(&f.from, "from", "", "with --job daily_stats: the first day to recompute (YYYY-MM-DD, UTC)")
	cmd.Flags().StringVar(&f.to, "to", "", "with --job daily_stats: the last day to recompute (YYYY-MM-DD, UTC, before today)")
	return cmd
}

// recomputeDays rewrites the daily_stats rows of [from, to].
func recomputeDays(ctx context.Context, cmd *cobra.Command, cfg *config.Config, from, to time.Time) error {
	set, closeFn, err := serve.NewDailyStats(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeFn()
	n, err := set.Range(ctx, from, to)
	if err != nil {
		return fmt.Errorf("sync-state: %w", err)
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "sync-state done: %d days of daily_stats recomputed\n", n)
	return err
}
