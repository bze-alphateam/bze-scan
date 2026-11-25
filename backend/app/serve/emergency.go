package serve

import (
	"time"

	circuittypes "cosmossdk.io/x/circuit/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	guardiantypes "github.com/bze-alphateam/bze/x/guardian/types"

	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/emergency"
	"github.com/bze-alphateam/bze-scan/backend/internal/grpcclient"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/params"
)

// EmergencyTimeout bounds each emergency state query.
const EmergencyTimeout = 5 * time.Second

// NewEmergency builds the emergency status route's reader over the local
// node (NODE_RPC_URL for the height, NODE_GRPC_ADDR for the queries): the
// guardian's switches, the circuit breaker, the halted denoms and the
// scheduled plan, cached emergency.DefaultTTL. The connection is lazy. Call
// closeFn when done.
func NewEmergency(cfg *config.Config, codec *chain.Codec) (reader *emergency.Reader, closeFn func(), err error) {
	conn, err := grpcclient.Dial(grpcclient.Config{
		Addr: cfg.NodeGRPCAddr, TLS: cfg.NodeGRPCTLS, Timeout: EmergencyTimeout,
	}, codec.GRPC())
	if err != nil {
		return nil, nil, err
	}
	reader = emergency.New(emergency.Config{}, emergency.Deps{
		Node:     node.New(cfg.NodeRPCURL),
		Guardian: guardiantypes.NewQueryClient(conn),
		Circuit:  circuittypes.NewQueryClient(conn),
		Halted:   params.NewGRPC(conn, codec),
		Upgrade:  upgradetypes.NewQueryClient(conn),
	})
	return reader, func() { _ = conn.Close() }, nil
}
