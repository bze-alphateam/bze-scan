// Package cli holds the cobra commands of the bze-scan binary: serve (the
// production process: HTTP API, status checker, live indexer, state sync and
// backfill), migrate (the database schema), backfill (the history
// backfill standalone), reindex (the repair tool), sync-state (one full
// state resync) and version (the build commit). It lives outside cmd/ so tests run the commands as users do.
package cli

import (
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/bze-alphateam/bze-scan/backend/config"
)

// NewRootCmd returns the bze-scan root command with every subcommand. The
// version command prints "dev" unless WithVersion says otherwise.
func NewRootCmd(opts ...Option) *cobra.Command {
	o := options{version: "dev"}
	for _, opt := range opts {
		opt(&o)
	}
	rootCmd := &cobra.Command{
		Use:           "bze-scan",
		Short:         "BZE block explorer backend",
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	rootCmd.AddCommand(newServeCmd(), newMigrateCmd(), newBackfillCmd(), newReindexCmd(), newSyncStateCmd(), newVersionCmd(o.version))
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
