package serve

import (
	"context"
	"fmt"
	"net/http"
	"time"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
	"github.com/jackc/pgx/v5/pgxpool"
	log "github.com/sirupsen/logrus"
	"golang.org/x/time/rate"

	tokenfactorytypes "github.com/bze-alphateam/bze/x/tokenfactory/types"
	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"

	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/activeset"
	"github.com/bze-alphateam/bze-scan/backend/internal/aggregator"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/chainregistry"
	"github.com/bze-alphateam/bze-scan/backend/internal/grpcclient"
	"github.com/bze-alphateam/bze-scan/backend/internal/keybase"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/params"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/chainstate"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/channels"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/dailystats"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/denoms"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/holders"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/nonvoters"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/orders"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/paramsnap"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/prices"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/proposals"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/registry"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/upgrades"
	"github.com/bze-alphateam/bze-scan/backend/internal/statesync/validators"
)

// outboundTimeout bounds one HTTPS call of the ticker jobs (the chain
// registry, the aggregator).
const outboundTimeout = 30 * time.Second

// keybaseTimeout bounds one validator logo lookup: the validators set looks
// them up one after the other.
const keybaseTimeout = 10 * time.Second

// NewStateSync builds the state sync over the local node's gRPC
// (NODE_GRPC_ADDR) with every registered set, on a pool of its own. Later
// domains register their sets here, in the order the start resync runs
// them: the IBC channels before the chain registry that names their
// counterparties and the denoms whose origin they give, the chain registry
// before the denoms it names, the holders and the prices after the denoms
// they fill, the proposals' non-voters after the proposals they read, the
// parameters after the proposals that explain their changes,
// the open orders' reconciliation with the node's order book anywhere (it
// reads nothing the others write), the chain state after them (it counts
// the validators and copies the native price), the daily statistics last
// (they read only the indexed tables). The chain registry runs only
// with both its URLs, the prices only with AGGREGATOR_URL, the validator
// logos only with a KeybaseURL (Load always sets one). The non-voters read
// the validator sets of the heights the local node pruned from the archive
// (ARCHIVE_RPC_URL), and the upgrade recovery the commits after an upgrade
// the same way, paced by BACKFILL_RATE_LIMIT. Call closeFn when done.
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
	staking := stakingtypes.NewQueryClient(conn)
	localNode := node.New(cfg.NodeRPCURL)
	// The by-height reads of the non-voters: the local node, else the archive.
	routed := activeset.NewRouted(localNode, node.New(cfg.ArchiveRPCURL))
	denomStore := denoms.NewPGStore(pool)
	tradebin := tradebintypes.NewQueryClient(conn)
	resyncs := &statesync.Deferred{}
	var logos validators.Logos
	if cfg.KeybaseURL != "" {
		logos = keybase.New(&http.Client{Timeout: keybaseTimeout}, cfg.KeybaseURL)
	}

	sets := []statesync.Set{
		validators.New(validators.Deps{
			Staking:  staking,
			Slashing: slashingtypes.NewQueryClient(conn),
			Store:    validators.NewPGStore(pool),
			Keys:     codec.InterfaceRegistry(),
			Logos:    logos,
		}, 0),
		channels.New(channels.Deps{
			Channel: channeltypes.NewQueryClient(conn),
			Client:  clienttypes.NewQueryClient(conn),
			Store:   channels.NewPGStore(pool),
			Misses:  resyncs,
		}, 0),
	}
	if cfg.ChainRegistryAPIURL != "" && cfg.ChainRegistryRawURL != "" {
		sets = append(sets, registry.New(registry.Deps{
			Registry: chainregistry.New(outbound, cfg.ChainRegistryAPIURL, cfg.ChainRegistryRawURL),
			Store:    registry.NewPGStore(pool),
			ChainID:  cfg.ChainID,
			Saved:    resyncs,
		}, 0))
	}
	sets = append(sets,
		denoms.New(denoms.Deps{
			Bank:         bank,
			TokenFactory: tokenfactorytypes.NewQueryClient(conn),
			Tradebin:     tradebin,
			Store:        denomStore,
			Transfer:     ibctransfertypes.NewQueryClient(conn),
			Lookup:       denomStore,
			Misses:       resyncs,
			ChainID:      cfg.ChainID,
		}, 0),
		proposals.New(proposals.Deps{
			Gov:     govv1.NewQueryClient(conn),
			Staking: staking,
			JSON:    codec,
			Store:   proposals.NewPGStore(pool),
		}, 0),
		nonvoters.New(nonvoters.Deps{
			Set:   activeset.New(routed, activeset.NewPGResolver(pool)),
			Node:  routed,
			Store: nonvoters.NewPGStore(pool),
		}, 0),
		upgrades.New(upgrades.Deps{
			Set:     activeset.New(routed, activeset.NewPGResolver(pool)),
			Node:    routed,
			Limiter: rate.NewLimiter(rate.Limit(cfg.BackfillRateLimit), 1),
			Store:   upgrades.NewPGStore(pool),
		}, upgrades.Config{Window: cfg.UpgradeRecoveryWindow}),
		paramsnap.New(paramsnap.Deps{
			Params: params.NewGRPC(conn, codec),
			Node:   localNode,
			Store:  paramsnap.NewPGStore(pool),
		}, 0),
		holders.New(holders.Deps{Bank: bank, Store: holders.NewPGStore(pool)}, 0),
		orders.New(orders.Deps{Node: localNode, Tradebin: tradebin, Store: orders.NewPGStore(pool)}, 0),
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
	sets = append(sets, chainstate.New(chainstate.Deps{
		Node:         localNode,
		Staking:      staking,
		Bank:         bank,
		Mint:         minttypes.NewQueryClient(conn),
		Distribution: distrtypes.NewQueryClient(conn),
		JSON:         codec,
		Store:        chainstate.NewPGStore(pool),
		Denom:        chain.BondDenom,
	}, 0), dailystats.New(dailystats.Deps{Store: dailystats.NewPGStore(pool)}))
	syncer = statesync.New(statesync.Config{Log: log.StandardLogger()}, statesync.NewPGJobStore(pool), sets...)
	resyncs.Bind(syncer)
	return syncer, func() {
		pool.Close()
		_ = conn.Close()
	}, nil
}

// NewDailyStats builds the daily statistics set alone, over the database
// only (sync-state --from --to recomputes a range of days with it). Call
// closeFn when done.
func NewDailyStats(ctx context.Context, cfg *config.Config) (set *dailystats.Set, closeFn func(), err error) {
	if err := cfg.RequireDatabase(); err != nil {
		return nil, nil, err
	}
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("daily stats pool: %w", err)
	}
	return dailystats.New(dailystats.Deps{Store: dailystats.NewPGStore(pool)}), pool.Close, nil
}

// NewHaltWatch builds the halt watch over the local node (NODE_RPC_URL), on
// a pool of its own. Call closeFn when done.
func NewHaltWatch(ctx context.Context, cfg *config.Config) (watch *upgrades.Watch, closeFn func(), err error) {
	if err := cfg.RequireDatabase(); err != nil {
		return nil, nil, err
	}
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("halt watch pool: %w", err)
	}
	local := node.New(cfg.NodeRPCURL)
	return upgrades.NewWatch(upgrades.WatchDeps{
		Node:  local,
		Set:   activeset.New(local, activeset.NewPGResolver(pool)),
		Store: upgrades.NewPGStore(pool),
	}, upgrades.WatchConfig{Interval: cfg.HaltWatchInterval}), pool.Close, nil
}
