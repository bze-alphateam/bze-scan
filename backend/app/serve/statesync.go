package serve

import (
	"context"
	"fmt"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/jackc/pgx/v5/pgxpool"
	log "github.com/sirupsen/logrus"

	tokenfactorytypes "github.com/bze-alphateam/bze/x/tokenfactory/types"
	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"

	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/grpcclient"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/denoms"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/validators"
)

// NewStateSync builds the state sync over the local node's gRPC
// (NODE_GRPC_ADDR) with every registered set, on a pool of its own. Later
// domains register their sets here. Call closeFn when done.
func NewStateSync(ctx context.Context, cfg *config.Config, codec *chain.Codec) (syncer *statesync.Syncer, closeFn func(), err error) {
	if err := cfg.RequireDatabase(); err != nil {
		return nil, nil, err
	}
	conn, err := grpcclient.Dial(grpcclient.Config{Addr: cfg.NodeGRPCAddr, TLS: cfg.NodeGRPCTLS}, codec.GRPC())
	if err != nil {
		return nil, nil, err
	}
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("state sync pool: %w", err)
	}
	syncer = statesync.New(statesync.Config{Log: log.StandardLogger()}, statesync.NewPGJobStore(pool),
		validators.New(validators.Deps{
			Staking:  stakingtypes.NewQueryClient(conn),
			Slashing: slashingtypes.NewQueryClient(conn),
			Store:    validators.NewPGStore(pool),
			Keys:     codec.InterfaceRegistry(),
		}, 0),
		denoms.New(denoms.Deps{
			Bank:         banktypes.NewQueryClient(conn),
			TokenFactory: tokenfactorytypes.NewQueryClient(conn),
			Tradebin:     tradebintypes.NewQueryClient(conn),
			Store:        denoms.NewPGStore(pool),
		}, 0),
	)
	return syncer, func() {
		pool.Close()
		_ = conn.Close()
	}, nil
}
