package chain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
)

// The expected addresses are the ones mainnet reports in the transfer events
// of the recorded fixtures.
func TestModuleAddress(t *testing.T) {
	assert.Equal(t, "bze17xpfvakm2amg962yls6f84z3kell8c5lda0te2", chain.ModuleAddress(chain.FeeCollector))
	assert.Equal(t, "bze1jv65s3grqf6v6jl3dp4t6c9t9rk99cd86mghmg", chain.ModuleAddress(chain.Distribution))
	assert.Equal(t, "bze10d07y265gmmuvt4z0w9aw880jnsr700j8xlwyy", chain.ModuleAddress(chain.Gov), "the authority of every proposal")
	assert.Equal(t, "bze1m3h30wlvsf8llruxtpukdvsy0km2kum844tn4h", chain.ModuleAddress(chain.Mint))
}
