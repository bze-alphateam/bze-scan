package serve

import (
	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bze-alphateam/bze-scan/backend/app/controller"
	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/grpcclient"
	"github.com/bze-alphateam/bze-scan/backend/internal/poolprice"
	"github.com/bze-alphateam/bze-scan/backend/internal/tickers"
)

// NewPoolPrices builds the LP token page's pool object: the pools' reserves
// and each pool's price average from the local node's gRPC (NODE_GRPC_ADDR)
// over a lazy connection of its own, the drift limits from the live
// parameters, the symbols and exponents from the denoms table. Call closeFn
// when done.
func NewPoolPrices(cfg *config.Config, codec *chain.Codec, db *pgxpool.Pool,
	live controller.LiveParams) (reader controller.PoolPriceReader, closeFn func(), err error) {
	conn, err := grpcclient.Dial(grpcclient.Config{
		Addr: cfg.NodeGRPCAddr, TLS: cfg.NodeGRPCTLS, Timeout: ParamsTimeout,
	}, codec.GRPC())
	if err != nil {
		return nil, nil, err
	}
	tradebin := tradebintypes.NewQueryClient(conn)
	svc := poolprice.New(poolprice.Deps{
		Pools:    tickers.NewNodePools(tradebin, banktypes.NewQueryClient(conn), 0, nil),
		Averages: poolprice.NewCached(tradebin, 0, nil),
		Params:   live,
		Metas:    poolprice.NewPGMetas(db),
	}, chain.BondDenom)
	return svc, func() { _ = conn.Close() }, nil
}
