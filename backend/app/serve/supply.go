package serve

import (
	"context"
	"time"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"

	"github.com/bze-alphateam/bze-scan/backend/app/repository"
	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/grpcclient"
	"github.com/bze-alphateam/bze-scan/backend/internal/supply"
)

// SupplyTimeout bounds each supply query; the last read is served
// meanwhile.
const SupplyTimeout = 5 * time.Second

// DenomDisplay reads the display data of denoms; *repository.Explorer
// satisfies it.
type DenomDisplay interface {
	Denoms(ctx context.Context, denoms []string) (map[string]repository.Denom, error)
}

// NewSupply builds the /aggregator/api/supply reader over the local node's gRPC
// (NODE_GRPC_ADDR) and the explorer's denoms. The connection is lazy. Call
// closeFn when done.
func NewSupply(cfg *config.Config, codec *chain.Codec, denoms DenomDisplay) (svc *supply.Service, closeFn func(), err error) {
	conn, err := grpcclient.Dial(grpcclient.Config{
		Addr: cfg.NodeGRPCAddr, TLS: cfg.NodeGRPCTLS, Timeout: SupplyTimeout,
	}, codec.GRPC())
	if err != nil {
		return nil, nil, err
	}
	svc = supply.New(supply.Deps{
		Bank:         banktypes.NewQueryClient(conn),
		Distribution: distrtypes.NewQueryClient(conn),
		Denoms:       exponents{denoms},
	})
	return svc, func() { _ = conn.Close() }, nil
}

// exponents adapts the explorer's denoms to supply.Denoms.
type exponents struct{ denoms DenomDisplay }

func (x exponents) Exponent(ctx context.Context, denom string) (int, bool, error) {
	got, err := x.denoms.Denoms(ctx, []string{denom})
	if err != nil {
		return 0, false, err
	}
	d, ok := got[denom]
	return d.Exponent, ok, nil
}
