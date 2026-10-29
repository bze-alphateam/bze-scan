package serve

import (
	"net/http"
	"time"

	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/coingecko"
)

// coingeckoTimeout bounds one CoinGecko call; the feed serves its last read
// meanwhile only to the callers that come after.
const coingeckoTimeout = 10 * time.Second

// NewPriceFeed returns the CoinGecko feed of cfg's price ids, behind
// /aggregator/api/prices and the prices set; nil without a CoinGecko host.
func NewPriceFeed(cfg *config.Config) *coingecko.Feed {
	if cfg.CoinGeckoHost == "" {
		return nil
	}
	client := coingecko.New(&http.Client{Timeout: coingeckoTimeout}, cfg.CoinGeckoHost)
	return coingecko.NewFeed(coingecko.FeedDeps{Source: client}, cfg.CoinGeckoPriceIDs)
}
