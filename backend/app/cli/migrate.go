package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/migrations"
)

func newMigrateCmd() *cobra.Command {
	up := newMigrateUpCmd()
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Manage the database schema (default: up)",
		Long: "Applies or reverts the explorer's database migrations. The database " +
			"must already hold the CometBFT psql sink schema. Without a " +
			"subcommand, migrate runs up.",
		Args: cobra.NoArgs,
		RunE: up.RunE,
	}
	cmd.AddCommand(up, newMigrateDownCmd(), newMigrateVersionCmd())
	return cmd
}

func newMigrateUpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Apply every pending migration, then the post-migration steps (partitions)",
		Long: "Brings the explorer schema to the latest version, then runs the " +
			"post-migration steps (the height partitions first). Safe to run on " +
			"every deploy: a second run changes nothing.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigrator(func(ctx context.Context, mg *migrations.Migrator) error {
				res, err := mg.Up(ctx)
				if err != nil {
					return err
				}
				log.WithFields(log.Fields{
					"version": res.Version,
					"applied": res.Applied,
					"steps":   res.Steps,
				}).Info("database migrated")
				return nil
			})
		},
	}
}

func newMigrateDownCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down <n>",
		Short: "Revert the last n migrations",
		Long: "Reverts the last n applied migrations. Down never touches the sink's " +
			"tables or rows; reverting every migration removes the explorer " +
			"schema and the notification trigger.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := strconv.Atoi(args[0])
			if err != nil || n < 1 {
				return fmt.Errorf("down needs a positive number of migrations, got %q", args[0])
			}
			return withMigrator(func(ctx context.Context, mg *migrations.Migrator) error {
				version, err := mg.Down(n)
				if err != nil {
					return err
				}
				log.WithFields(log.Fields{"reverted": n, "version": version}).Info("migrations reverted")
				return nil
			})
		},
	}
}

func newMigrateVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the current schema version (0 when none is applied)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMigrator(func(ctx context.Context, mg *migrations.Migrator) error {
				version, dirty, err := mg.Version()
				if err != nil {
					return err
				}
				out := strconv.FormatUint(uint64(version), 10)
				if dirty {
					out += " (dirty)"
				}
				_, err = fmt.Fprintln(cmd.OutOrStdout(), out)
				return err
			})
		},
	}
}

// withMigrator loads the configuration (DATABASE_URL required), opens a
// migrator and runs fn under a context cancelled by SIGINT/SIGTERM.
func withMigrator(fn func(ctx context.Context, mg *migrations.Migrator) error) error {
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
	mg, err := migrations.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer func() { _ = mg.Close() }()
	return fn(ctx, mg)
}
