package serve

import (
	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/jackc/pgx/v5/pgxpool"
	log "github.com/sirupsen/logrus"

	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/candles"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/coingecko"
	"github.com/bze-alphateam/bze-scan/backend/internal/dexunits"
	"github.com/bze-alphateam/bze-scan/backend/internal/grpcclient"
	"github.com/bze-alphateam/bze-scan/backend/internal/tickers"
	"github.com/bze-alphateam/bze-scan/backend/internal/tradehistory"
)

// NewTickers builds the DEX tickers of the aggregator-compatible
// /aggregator/api/dex/tickers: the order books of books, the pools and LP
// supplies of the local node's gRPC (NODE_GRPC_ADDR) over a lazy connection
// of their own, the candles and trades of pool, CoinGecko's prices of feed
// (nil: no fallback). Call closeFn when done.
func NewTickers(cfg *config.Config, codec *chain.Codec, books tickers.Books, pool *pgxpool.Pool,
	feed *coingecko.Feed) (*tickers.Service, func(), error) {
	conn, err := grpcclient.Dial(grpcclient.Config{
		Addr: cfg.NodeGRPCAddr, TLS: cfg.NodeGRPCTLS, Timeout: OrderBookTimeout,
	}, codec.GRPC())
	if err != nil {
		return nil, nil, err
	}
	svc := newTickers(cfg, books, tradebintypes.NewQueryClient(conn), banktypes.NewQueryClient(conn), pool, feed)
	return svc, func() { _ = conn.Close() }, nil
}

// newTickers builds the tickers over the node's tradebin and bank queries.
func newTickers(cfg *config.Config, books tickers.Books, tradebin tickers.PoolNode, bank tickers.SupplyNode,
	pool *pgxpool.Pool, feed *coingecko.Feed) *tickers.Service {
	store := tradehistory.NewPGStore(pool)
	deps := tickers.Deps{
		Books:   books,
		Candles: candles.NewPGReader(pool),
		Trades: tradehistory.New(tradehistory.Deps{Store: store, Exponents: dexunits.NewPGStore(pool),
			Log: log.StandardLogger()}),
		Heights:   store,
		Pools:     tickers.NewNodePools(tradebin, bank, 0, nil),
		Exponents: dexunits.NewPGStore(pool),
		Log:       log.StandardLogger(),
	}
	// A nil interface without a feed, never a typed nil.
	if feed != nil {
		deps.CoinGecko = feed
	}
	return tickers.New(tickers.Config{NativeDenom: chain.BondDenom, USDCDenom: cfg.USDCDenom}, deps)
}
