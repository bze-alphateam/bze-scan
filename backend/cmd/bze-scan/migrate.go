package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/bze-alphateam/bze-scan/backend/app/migrations"
	"github.com/bze-alphateam/bze-scan/backend/config"
)

func newMigrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Apply the database migrations and create the height partitions",
		Long: "Brings the explorer schema to the latest version, creates the height " +
			"partitions up to the highest known height plus the look-ahead and " +
			"installs the notification trigger on the psql sink's blocks table. " +
			"The database must already hold the CometBFT psql sink schema. " +
			"Safe to run on every deploy: a second run changes nothing.",
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
			return runMigrate(ctx, cfg)
		},
	}
}

func runMigrate(ctx context.Context, cfg *config.Config) error {
	res, err := migrations.Run(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	log.WithFields(log.Fields{
		"version":         res.Version,
		"applied":         res.Applied,
		"partitions_from": res.PartitionsFrom,
		"partitions_to":   res.PartitionsTo,
	}).Info("database migrated")
	return nil
}
