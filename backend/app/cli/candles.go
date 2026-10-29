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

func newCandlesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "candles",
		Short: "Maintain the DEX candles (explorer.candles)",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newCandlesRebuildCmd())
	return cmd
}

func newCandlesRebuildCmd() *cobra.Command {
	var market string
	cmd := &cobra.Command{
		Use:   "rebuild [--market <id>]",
		Short: "Recompute the DEX candles from the stored trades",
		Long: "Recomputes every candle (5, 15, 60, 240 and 1440 minutes) of every order book market and " +
			"liquidity pool with a stored trade, or only of --market (a market id base/quote or a pool id " +
			"base_quote), from explorer.order_fills and explorer.swaps: one transaction per market, the " +
			"candles of days without trades deleted. Nothing is read from the node; serve may keep running. " +
			"Exits 0 on success, 1 on a configuration or database error.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			configureLogging(cfg)

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			r, closeDB, err := serve.NewCandleRebuilder(ctx, cfg)
			if err != nil {
				return err
			}
			defer closeDB()

			started := time.Now()
			markets, trades, err := r.All(ctx, market)
			if _, perr := fmt.Fprintf(cmd.OutOrStdout(), "rebuilt %d markets from %d trades in %s\n",
				markets, trades, time.Since(started).Round(time.Millisecond)); perr != nil && err == nil {
				err = perr
			}
			return err
		},
	}
	cmd.Flags().StringVar(&market, "market", "", "rebuild only this market or pool id")
	return cmd
}
