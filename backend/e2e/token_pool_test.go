//go:build e2e

package e2e_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
)

const usdcLPDenom = "ulp_" + usdcDenom + "_ubze"

// The LP token page carries the pool's price state: the recorded VDL/BZE
// pool holds 1000 VDL and 2000 BZE (spot 2) against a recorded average of
// 1.8, 11.1% away, past both limits (10% and 3%). The USDC.n/BZE pool has no
// recorded average, as on a node before v8.3.0.
func TestTheLPTokenPageCarriesThePoolPrice(t *testing.T) {
	e := newSyncEnv(t)
	base := e.serve(t, h316)
	waitFor(t, 10*time.Second, func() bool {
		return e.count(t, `SELECT count(*) FROM explorer.denoms`) == recordedDenoms
	}, "the denoms")

	vdl := fetch(t, base+"/api/v1/tokens/"+url.PathEscape(vdlLPDenom))
	require.Equal(t, http.StatusOK, vdl.status)
	lp := into[dto.Token](t, vdl)
	require.NotNil(t, lp.Pool)
	assert.Equal(t, vdlDenom+"_ubze", lp.Pool.PoolID)
	assert.Equal(t, "1000000000", lp.Pool.Reserves.Base.Reserve)
	require.NotNil(t, lp.Pool.Price)
	assert.Equal(t, "2.000000000000000000", lp.Pool.Price.Chain)
	require.NotNil(t, lp.Pool.AvgPrice)
	assert.Equal(t, "1.800000000000000000", lp.Pool.AvgPrice.Chain)
	require.NotNil(t, lp.Pool.AvgLastHeight)
	assert.EqualValues(t, 4405100, *lp.Pool.AvgLastHeight)
	require.NotNil(t, lp.Pool.DriftPct)
	assert.Equal(t, "11.1", *lp.Pool.DriftPct)
	require.NotNil(t, lp.Pool.HalfLifeBlocks)
	assert.EqualValues(t, 20, *lp.Pool.HalfLifeBlocks)
	require.NotNil(t, lp.Pool.FeesInBZE)
	assert.True(t, *lp.Pool.FeesInBZE)
	require.NotNil(t, lp.Pool.ModuleSwapsHeld)
	assert.True(t, *lp.Pool.ModuleSwapsHeld)

	res := fetch(t, base+"/api/v1/tokens/"+url.PathEscape(usdcLPDenom))
	require.Equal(t, http.StatusOK, res.status)
	usdc := into[dto.Token](t, res)
	require.NotNil(t, usdc.Pool)
	assert.Nil(t, usdc.Pool.AvgPrice, "no average on the node")
	assert.Nil(t, usdc.Pool.FeesInBZE)
	assert.Nil(t, usdc.Pool.ModuleSwapsHeld)

	// Other denoms have no pool object.
	native := into[dto.Token](t, fetch(t, base+"/api/v1/tokens/ubze"))
	assert.Nil(t, native.Pool)
}
