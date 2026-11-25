package serve

import (
	"time"

	ratelimittypes "github.com/cosmos/ibc-apps/modules/rate-limiting/v10/types"

	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/grpcclient"
	"github.com/bze-alphateam/bze-scan/backend/internal/ratelimits"
)

// RateLimitsTimeout bounds each live rate limit query.
const RateLimitsTimeout = 5 * time.Second

// NewRateLimits builds the IBC rate limits reader over the local node's gRPC
// (NODE_GRPC_ADDR), cached ratelimits.DefaultTTL. The connection is lazy.
// Call closeFn when done.
func NewRateLimits(cfg *config.Config, codec *chain.Codec) (reader *ratelimits.Service, closeFn func(), err error) {
	conn, err := grpcclient.Dial(grpcclient.Config{
		Addr: cfg.NodeGRPCAddr, TLS: cfg.NodeGRPCTLS, Timeout: RateLimitsTimeout,
	}, codec.GRPC())
	if err != nil {
		return nil, nil, err
	}
	return ratelimits.New(ratelimittypes.NewQueryClient(conn), 0, nil), func() { _ = conn.Close() }, nil
}
