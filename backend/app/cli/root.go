// Package cli holds the cobra commands of the bze-scan binary: serve (the
// production process: HTTP API, status checker, live indexer, state sync and
// backfill), migrate (the database schema), backfill (the history
// backfill standalone), reindex (the repair tool) and sync-state (one full
// state resync). It lives outside cmd/ so tests run the commands as users do.
package cli

import (
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/bze-alphateam/bze-scan/backend/config"
)

// NewRootCmd returns the bze-scan root command with every subcommand.
func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "bze-scan",
		Short:         "BZE block explorer backend",
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	rootCmd.AddCommand(newServeCmd(), newMigrateCmd(), newBackfillCmd(), newReindexCmd(), newSyncStateCmd())
	return rootCmd
}

// configureLogging applies LOG_LEVEL and LOG_FORMAT (already validated by
// config.Load) to logrus.
func configureLogging(cfg *config.Config) {
	if lvl, err := log.ParseLevel(cfg.LogLevel); err == nil {
		log.SetLevel(lvl)
	}
	if cfg.LogFormat == config.LogFormatJSON {
		log.SetFormatter(&log.JSONFormatter{})
	} else {
		log.SetFormatter(&log.TextFormatter{FullTimestamp: true})
	}
}
