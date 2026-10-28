package serve

import (
	"time"

	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/dexunits"
	"github.com/bze-alphateam/bze-scan/backend/internal/grpcclient"
	"github.com/bze-alphateam/bze-scan/backend/internal/orderbook"
)

// OrderBookTimeout bounds each tradebin query of the order books.
const OrderBookTimeout = 5 * time.Second

// NewOrderBook builds the DEX order books of the aggregator-compatible
// /api/dex/orders: the local node's tradebin queries (NODE_GRPC_ADDR) over a
// lazy connection of their own, exponents from explorer.denoms through pool.
// Call closeFn when done.
func NewOrderBook(cfg *config.Config, codec *chain.Codec, pool *pgxpool.Pool) (*orderbook.Service, func(), error) {
	conn, err := grpcclient.Dial(grpcclient.Config{
		Addr: cfg.NodeGRPCAddr, TLS: cfg.NodeGRPCTLS, Timeout: OrderBookTimeout,
	}, codec.GRPC())
	if err != nil {
		return nil, nil, err
	}
	books := orderbook.New(orderbook.Config{}, orderbook.Deps{
		Node:      tradebintypes.NewQueryClient(conn),
		Exponents: dexunits.NewPGStore(pool),
	})
	return books, func() { _ = conn.Close() }, nil
}
