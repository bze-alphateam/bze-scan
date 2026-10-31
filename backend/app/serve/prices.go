package serve

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/coingecko"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/prices"
)

// coingeckoTimeout bounds one CoinGecko call; the feed serves its last read
// meanwhile.
const coingeckoTimeout = 10 * time.Second

// NewPriceFeed returns the CoinGecko feed of BZE's id and the registry ids
// of pool's denoms, behind /aggregator/api/prices and the prices set; nil
// without a CoinGecko host.
func NewPriceFeed(cfg *config.Config, pool *pgxpool.Pool) *coingecko.Feed {
	if cfg.CoinGeckoHost == "" {
		return nil
	}
	client := coingecko.New(&http.Client{Timeout: coingeckoTimeout}, cfg.CoinGeckoHost)
	return coingecko.NewFeed(coingecko.FeedDeps{Source: client, IDs: prices.NewRegistryIDs(pool, cfg.ChainID)})
}
