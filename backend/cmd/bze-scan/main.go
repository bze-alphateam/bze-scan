// Command bze-scan is the BZE block explorer backend. Subcommands: serve (the
// production process: HTTP API and, as later work lands, the live indexer,
// state sync, status checker and backfill).
package main

import (
	"os"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/bze-alphateam/bze-scan/backend/config"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "bze-scan",
		Short:         "BZE block explorer backend",
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	rootCmd.AddCommand(newServeCmd())
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
