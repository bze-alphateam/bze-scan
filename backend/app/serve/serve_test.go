package serve

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/config"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

func indexerConfig(nodeURL, chainID string) *config.Config {
	return &config.Config{
		HTTPAddr:       "127.0.0.1:0",
		LogLevel:       "warn",
		LogFormat:      config.LogFormatText,
		DatabaseURL:    "postgres://bze@127.0.0.1:1/none?sslmode=disable",
		NodeRPCURL:     nodeURL,
		ChainID:        chainID,
		IndexerEnabled: true,
	}
}

func TestRefusesANodeOfAnotherChain(t *testing.T) {
	n := fakenode.New(t)

	err := Run(context.Background(), indexerConfig(n.URL, "beezee-testnet"), Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `serves chain "beezee-1"`)
	assert.Contains(t, err.Error(), `CHAIN_ID is "beezee-testnet"`)
	assert.Equal(t, 0, n.Requests(fakenode.RouteBlock), "nothing is indexed")
}

func TestRefusesAnUnreachableNode(t *testing.T) {
	n := fakenode.New(t)
	n.Close()

	err := Run(context.Background(), indexerConfig(n.URL, "beezee-1"), Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "node status")
}

func TestIndexerRequiresDatabaseURL(t *testing.T) {
	cfg := indexerConfig("http://127.0.0.1:1", "beezee-1")
	cfg.DatabaseURL = ""

	err := Run(context.Background(), cfg, Options{})
	assert.ErrorIs(t, err, config.ErrDatabaseURLRequired)
}
