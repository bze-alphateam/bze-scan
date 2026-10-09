package grpcclient_test

import (
	"context"
	"net"
	"testing"
	"time"

	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/grpcclient"
)

// stall serves a gRPC server whose every call waits until the client gives
// up.
func stall(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := grpc.NewServer(grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
		<-stream.Context().Done()
		return stream.Context().Err()
	}))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return lis.Addr().String()
}

func TestEveryCallIsBounded(t *testing.T) {
	codec, err := chain.NewCodec()
	require.NoError(t, err)
	conn, err := grpcclient.Dial(grpcclient.Config{Addr: stall(t), Timeout: 100 * time.Millisecond}, codec.GRPC())
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	start := time.Now()
	_, err = stakingtypes.NewQueryClient(conn).Pool(context.Background(), &stakingtypes.QueryPoolRequest{})
	assert.Equal(t, codes.DeadlineExceeded, status.Code(err))
	assert.Less(t, time.Since(start), 5*time.Second)
}

func TestDialIsLazy(t *testing.T) {
	codec, err := chain.NewCodec()
	require.NoError(t, err)
	// Nothing listens there: Dial succeeds, the call fails.
	conn, err := grpcclient.Dial(grpcclient.Config{Addr: "127.0.0.1:1", TLS: true}, codec.GRPC())
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	_, err = stakingtypes.NewQueryClient(conn).Pool(context.Background(), &stakingtypes.QueryPoolRequest{})
	assert.Equal(t, codes.Unavailable, status.Code(err))
}
