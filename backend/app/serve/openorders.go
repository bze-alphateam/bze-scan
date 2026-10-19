package serve

import (
	"time"

	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/grpcclient"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/openorders"
	"github.com/bze-alphateam/bze-scan/backend/internal/ordercoverage"
)

// OpenOrdersTimeout bounds each tradebin query of the open orders route.
const OpenOrdersTimeout = 3 * time.Second

// NewOpenOrders builds the open orders route's reader and the coverage
// checker it relies on (run it as a component). It reads the orders table
// through pool when the coverage of CHAIN_ID is complete, and otherwise the
// local node (NODE_RPC_URL for the height, NODE_GRPC_ADDR for the queries),
// falling back to the archive's /abci_query (ARCHIVE_RPC_URL). The gRPC
// connection is lazy. coverageCfg tunes the checker (its zero value is
// production); its ChainID is CHAIN_ID. Call closeFn when done.
func NewOpenOrders(cfg *config.Config, codec *chain.Codec, pool *pgxpool.Pool, coverageCfg ordercoverage.Config) (
	reader *openorders.Reader, coverage *ordercoverage.Coverage, closeFn func(), err error) {
	conn, err := grpcclient.Dial(grpcclient.Config{
		Addr: cfg.NodeGRPCAddr, TLS: cfg.NodeGRPCTLS, Timeout: OpenOrdersTimeout,
	}, codec.GRPC())
	if err != nil {
		return nil, nil, nil, err
	}
	coverageCfg.ChainID = cfg.ChainID
	coverage = ordercoverage.New(coverageCfg, ordercoverage.NewPGStore(pool))
	reader = openorders.New(openorders.Config{}, openorders.Deps{
		Index:    openorders.NewPGStore(pool),
		Coverage: coverage,
		Local:    openorders.NewGRPCSource(node.New(cfg.NodeRPCURL), tradebintypes.NewQueryClient(conn)),
		Archive:  openorders.NewABCISource(node.New(cfg.ArchiveRPCURL)),
	})
	return reader, coverage, func() { _ = conn.Close() }, nil
}
