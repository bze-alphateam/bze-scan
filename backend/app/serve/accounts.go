package serve

import (
	"time"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/chainstate"
	"github.com/bze-alphateam/bze-scan/backend/internal/grpcclient"
)

// AccountStateTimeout bounds each live account query: the page renders
// without the live part rather than wait on a slow node.
const AccountStateTimeout = 3 * time.Second

// NewAccountState builds the live account reader over the local node's gRPC
// (NODE_GRPC_ADDR). The connection is lazy: a node that is down fails the
// reads, not the start. Call closeFn when done.
func NewAccountState(cfg *config.Config, codec *chain.Codec) (reader *chainstate.Reader, closeFn func(), err error) {
	conn, err := grpcclient.Dial(grpcclient.Config{
		Addr: cfg.NodeGRPCAddr, TLS: cfg.NodeGRPCTLS, Timeout: AccountStateTimeout,
	}, codec.GRPC())
	if err != nil {
		return nil, nil, err
	}
	reader = chainstate.New(chainstate.Config{}, chainstate.Deps{
		Bank:         banktypes.NewQueryClient(conn),
		Staking:      stakingtypes.NewQueryClient(conn),
		Distribution: distrtypes.NewQueryClient(conn),
	})
	return reader, func() { _ = conn.Close() }, nil
}
