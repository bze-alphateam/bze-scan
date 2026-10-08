package chain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The expected addresses are the ones mainnet reports in the transfer events
// of the recorded fixtures.
func TestModuleAddress(t *testing.T) {
	assert.Equal(t, "bze17xpfvakm2amg962yls6f84z3kell8c5lda0te2", ModuleAddress(FeeCollector))
	assert.Equal(t, "bze1jv65s3grqf6v6jl3dp4t6c9t9rk99cd86mghmg", ModuleAddress(Distribution))
	assert.Equal(t, "bze1m3h30wlvsf8llruxtpukdvsy0km2kum844tn4h", ModuleAddress(Mint))
}
