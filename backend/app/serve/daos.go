package serve

import (
	"time"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	daotypes "github.com/bze-alphateam/bze/x/dao/types"
	rewardstypes "github.com/bze-alphateam/bze/x/rewards/types"

	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/chainstate"
	"github.com/bze-alphateam/bze-scan/backend/internal/daostate"
	"github.com/bze-alphateam/bze-scan/backend/internal/grpcclient"
)

// DaoStateTimeout bounds each live DAO query: the page renders without the
// live part rather than wait on a slow node.
const DaoStateTimeout = 3 * time.Second

// NewDaoState builds the live DAO reader and the treasury balances reader
// over the local node's gRPC (NODE_GRPC_ADDR). The connection is lazy: a node
// that is down fails the reads, not the start; a node that has not upgraded
// to chain v8.3.0 answers Unimplemented, which the reader reports as not
// supported. Call closeFn when done.
func NewDaoState(cfg *config.Config, codec *chain.Codec) (reader *daostate.Reader, treasury *chainstate.Reader, closeFn func(), err error) {
	conn, err := grpcclient.Dial(grpcclient.Config{
		Addr: cfg.NodeGRPCAddr, TLS: cfg.NodeGRPCTLS, Timeout: DaoStateTimeout,
	}, codec.GRPC())
	if err != nil {
		return nil, nil, nil, err
	}
	reader = daostate.New(daostate.Config{}, daostate.Deps{
		DAO:     daotypes.NewQueryClient(conn),
		Rewards: rewardstypes.NewQueryClient(conn),
	})
	// Only the bank's balances are read: the other queries stay unset.
	treasury = chainstate.New(chainstate.Config{}, chainstate.Deps{Bank: banktypes.NewQueryClient(conn)})
	return reader, treasury, func() { _ = conn.Close() }, nil
}
