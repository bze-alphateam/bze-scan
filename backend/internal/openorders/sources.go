package openorders

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	tradebintypes "github.com/bze-alphateam/bze/x/tradebin/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	grpctypes "github.com/cosmos/cosmos-sdk/types/grpc"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
)

// Source is one node's tradebin queries at a height.
type Source interface {
	// Height returns the node's latest height, which every read of one
	// answer is pinned to.
	Height(ctx context.Context) (int64, error)
	UserMarketOrders(ctx context.Context, height int64, in *tradebintypes.QueryUserMarketOrdersRequest) (*tradebintypes.QueryUserMarketOrdersResponse, error)
	MarketOrder(ctx context.Context, height int64, in *tradebintypes.QueryMarketOrderRequest) (*tradebintypes.QueryMarketOrderResponse, error)
	AllMarkets(ctx context.Context, height int64, in *tradebintypes.QueryAllMarketsRequest) (*tradebintypes.QueryAllMarketsResponse, error)
}

// StatusNode is a node's CometBFT RPC; *node.Client satisfies it.
type StatusNode interface {
	Status(ctx context.Context) (*node.Status, []byte, error)
}

// Tradebin is the part of the tradebin query client the gRPC source uses;
// tradebintypes.QueryClient satisfies it.
type Tradebin interface {
	UserMarketOrders(ctx context.Context, in *tradebintypes.QueryUserMarketOrdersRequest, opts ...grpc.CallOption) (*tradebintypes.QueryUserMarketOrdersResponse, error)
	MarketOrder(ctx context.Context, in *tradebintypes.QueryMarketOrderRequest, opts ...grpc.CallOption) (*tradebintypes.QueryMarketOrderResponse, error)
	AllMarkets(ctx context.Context, in *tradebintypes.QueryAllMarketsRequest, opts ...grpc.CallOption) (*tradebintypes.QueryAllMarketsResponse, error)
}

// ABCINode is a node's CometBFT RPC with its ABCI query; *node.Client
// satisfies it.
type ABCINode interface {
	StatusNode
	Query(ctx context.Context, path string, req, resp gogoproto.Message, height int64) error
}

// NewGRPCSource reads through a node's gRPC (the local node), each call
// pinned with the x-cosmos-block-height header to the height its RPC's
// /status reports.
func NewGRPCSource(rpc StatusNode, client Tradebin) Source {
	return grpcSource{rpc: rpc, client: client}
}

// NewABCISource reads through a node's /abci_query (the archive node) at
// the height its /status reports.
func NewABCISource(n ABCINode) Source {
	return abciSource{node: n}
}

type grpcSource struct {
	rpc    StatusNode
	client Tradebin
}

func (s grpcSource) Height(ctx context.Context) (int64, error) { return statusHeight(ctx, s.rpc) }

func at(ctx context.Context, height int64) context.Context {
	return metadata.AppendToOutgoingContext(ctx, grpctypes.GRPCBlockHeightHeader, strconv.FormatInt(height, 10))
}

func (s grpcSource) UserMarketOrders(ctx context.Context, height int64,
	in *tradebintypes.QueryUserMarketOrdersRequest) (*tradebintypes.QueryUserMarketOrdersResponse, error) {
	return s.client.UserMarketOrders(at(ctx, height), in)
}

func (s grpcSource) MarketOrder(ctx context.Context, height int64,
	in *tradebintypes.QueryMarketOrderRequest) (*tradebintypes.QueryMarketOrderResponse, error) {
	return s.client.MarketOrder(at(ctx, height), in)
}

func (s grpcSource) AllMarkets(ctx context.Context, height int64,
	in *tradebintypes.QueryAllMarketsRequest) (*tradebintypes.QueryAllMarketsResponse, error) {
	return s.client.AllMarkets(at(ctx, height), in)
}

type abciSource struct {
	node ABCINode
}

// The tradebin query paths.
const (
	pathUserMarketOrders = "/bze.tradebin.Query/UserMarketOrders"
	pathMarketOrder      = "/bze.tradebin.Query/MarketOrder"
	pathAllMarkets       = "/bze.tradebin.Query/AllMarkets"
)

func (s abciSource) Height(ctx context.Context) (int64, error) { return statusHeight(ctx, s.node) }

func (s abciSource) UserMarketOrders(ctx context.Context, height int64,
	in *tradebintypes.QueryUserMarketOrdersRequest) (*tradebintypes.QueryUserMarketOrdersResponse, error) {
	out := &tradebintypes.QueryUserMarketOrdersResponse{}
	return out, s.node.Query(ctx, pathUserMarketOrders, in, out, height)
}

func (s abciSource) MarketOrder(ctx context.Context, height int64,
	in *tradebintypes.QueryMarketOrderRequest) (*tradebintypes.QueryMarketOrderResponse, error) {
	out := &tradebintypes.QueryMarketOrderResponse{}
	return out, s.node.Query(ctx, pathMarketOrder, in, out, height)
}

func (s abciSource) AllMarkets(ctx context.Context, height int64,
	in *tradebintypes.QueryAllMarketsRequest) (*tradebintypes.QueryAllMarketsResponse, error) {
	out := &tradebintypes.QueryAllMarketsResponse{}
	return out, s.node.Query(ctx, pathAllMarkets, in, out, height)
}

func statusHeight(ctx context.Context, n StatusNode) (int64, error) {
	st, _, err := n.Status(ctx)
	if err != nil {
		return 0, fmt.Errorf("node status: %w", err)
	}
	return st.LatestBlockHeight, nil
}

// unavailable reports whether err says the node cannot be reached: an RPC
// transport error (node.ErrUnavailable), a gRPC Unavailable, or a gRPC call
// that timed out while the request itself was still running.
func unavailable(ctx context.Context, err error) bool {
	if errors.Is(err, node.ErrUnavailable) {
		return true
	}
	switch status.Code(err) {
	case codes.Unavailable:
		return true
	case codes.DeadlineExceeded:
		return ctx.Err() == nil
	default:
		return false
	}
}

// notFound reports whether err is the chain's "no such order": gRPC
// NotFound, which /abci_query reports as the SDK's key-not-found code.
func notFound(err error) bool {
	if status.Code(err) == codes.NotFound {
		return true
	}
	var abci *node.ABCIError
	return errors.As(err, &abci) &&
		abci.Codespace == sdkerrors.ErrKeyNotFound.Codespace() && abci.Code == sdkerrors.ErrKeyNotFound.ABCICode()
}
