package serve

import (
	"context"
	"fmt"
	"net/http"
	"time"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	"github.com/jackc/pgx/v5/pgxpool"
	log "github.com/sirupsen/logrus"

	tokenfactorytypes "github.com/bze-alphateam/bze/x/tokenfactory/types"
	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"

	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/aggregator"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/chainregistry"
	"github.com/bze-alphateam/bze-scan/backend/internal/grpcclient"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/denoms"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/holders"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/prices"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/proposals"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/registry"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/validators"
)

// outboundTimeout bounds one HTTPS call of the ticker jobs (the chain
// registry, the aggregator).
const outboundTimeout = 30 * time.Second

// NewStateSync builds the state sync over the local node's gRPC
// (NODE_GRPC_ADDR) with every registered set, on a pool of its own. Later
// domains register their sets here, in the order the start resync runs
// them: the chain registry before the denoms it names, the holders and the
// prices after the denoms they fill. The chain registry runs only with both
// its URLs, the prices only with AGGREGATOR_URL. Call closeFn when done.
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
	outbound := &http.Client{Timeout: outboundTimeout}
	bank := banktypes.NewQueryClient(conn)
	denomStore := denoms.NewPGStore(pool)
	misses := &statesync.Deferred{}

	sets := []statesync.Set{
		validators.New(validators.Deps{
			Staking:  stakingtypes.NewQueryClient(conn),
			Slashing: slashingtypes.NewQueryClient(conn),
			Store:    validators.NewPGStore(pool),
			Keys:     codec.InterfaceRegistry(),
		}, 0),
	}
	if cfg.ChainRegistryAPIURL != "" && cfg.ChainRegistryRawURL != "" {
		sets = append(sets, registry.New(registry.Deps{
			Registry: chainregistry.New(outbound, cfg.ChainRegistryAPIURL, cfg.ChainRegistryRawURL),
			Store:    registry.NewPGStore(pool),
			ChainID:  cfg.ChainID,
		}, 0))
	}
	sets = append(sets,
		denoms.New(denoms.Deps{
			Bank:         bank,
			TokenFactory: tokenfactorytypes.NewQueryClient(conn),
			Tradebin:     tradebintypes.NewQueryClient(conn),
			Store:        denomStore,
			Transfer:     ibctransfertypes.NewQueryClient(conn),
			Lookup:       denomStore,
			Misses:       misses,
		}, 0),
		proposals.New(proposals.Deps{
			Gov:     govv1.NewQueryClient(conn),
			Staking: stakingtypes.NewQueryClient(conn),
			JSON:    codec,
			Store:   proposals.NewPGStore(pool),
		}, 0),
		holders.New(holders.Deps{Bank: bank, Store: holders.NewPGStore(pool)}, 0),
	)
	if cfg.AggregatorURL != "" {
		sets = append(sets, prices.New(prices.Deps{
			Source:       aggregator.New(outbound, cfg.AggregatorURL),
			Store:        prices.NewPGStore(pool),
			ChainID:      cfg.ChainID,
			Denom:        chain.BondDenom,
			ChangeMarket: cfg.PriceChangeMarket,
		}, 0))
	}
	syncer = statesync.New(statesync.Config{Log: log.StandardLogger()}, statesync.NewPGJobStore(pool), sets...)
	misses.Bind(syncer)
	return syncer, func() {
		pool.Close()
		_ = conn.Close()
	}, nil
}
