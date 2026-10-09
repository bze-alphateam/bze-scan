// Package grpcclient connects to the local node's gRPC server, which the
// state sync queries for current state (validators, proposals, denoms…).
// Only the local node is ever dialled, never a public one.
package grpcclient

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding"
)

// DefaultTimeout bounds every call that has no earlier deadline.
const DefaultTimeout = 10 * time.Second

// Config of a connection.
type Config struct {
	// Addr is host:port of the node's gRPC server.
	Addr string
	// TLS dials with TLS (system roots) instead of plaintext.
	TLS bool
	// Timeout bounds each call; zero is DefaultTimeout.
	Timeout time.Duration
}

// Dial returns a connection to cfg.Addr that encodes with codec (the chain's
// codec, so Any values in responses are resolved) and bounds every call by
// cfg.Timeout. It connects lazily: an unreachable node fails the calls, not
// Dial. The generated query clients of the SDK, ibc-go and the bze modules
// take the connection as is (stakingtypes.NewQueryClient(conn), …).
func Dial(cfg Config, codec encoding.Codec) (*grpc.ClientConn, error) {
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	creds := insecure.NewCredentials()
	if cfg.TLS {
		creds = credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
	}
	conn, err := grpc.NewClient(cfg.Addr,
		grpc.WithTransportCredentials(creds),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(codec)),
		grpc.WithUnaryInterceptor(timeout(cfg.Timeout)),
	)
	if err != nil {
		return nil, fmt.Errorf("gRPC %s: %w", cfg.Addr, err)
	}
	return conn, nil
}

// timeout bounds a call by d unless its context ends earlier.
func timeout(d time.Duration) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx, cancel := context.WithTimeout(ctx, d)
		defer cancel()
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}
