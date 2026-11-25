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

// A factory token whose admin kept minting reads mintable true and an empty
// config; the renounced one beside it (the VDL token) reads false.
func TestTokenSettingsOfAMintableToken(t *testing.T) {
	e := newSyncEnv(t)
	base := e.serve(t, hOrders)
	waitFor(t, 10*time.Second, func() bool {
		return e.count(t, `SELECT count(*) FROM explorer.denoms WHERE mintable IS NOT NULL`) > 0
	}, "the denoms set read the settings")

	r := fetch(t, base+"/api/v1/tokens/"+url.PathEscape(coreyDenom))
	require.Equal(t, http.StatusOK, r.status, string(r.body))
	tok := into[dto.Token](t, r)
	require.NotNil(t, tok.Mintable)
	assert.True(t, *tok.Mintable)
	require.NotNil(t, tok.Config)
	assert.Nil(t, tok.Config.CommunityPool)
	assert.Equal(t, "0", tok.Config.TransferFee)
	assert.Equal(t, "0", tok.Config.GasSurcharge)

	native := into[dto.Token](t, fetch(t, base+"/api/v1/tokens/ubze"))
	assert.Nil(t, native.Mintable, "not a factory token")
	assert.Nil(t, native.Config)
}

// A resync of one denom (what a config_changed event marks dirty) picks up
// the settings the admin set on the chain. The VDL token is used: it has the
// bank metadata a single-denom resync needs.
func TestAResyncOfADenomPicksUpItsNewConfig(t *testing.T) {
	e := newSyncEnv(t)
	base := e.serve(t, hOrders)
	waitFor(t, 10*time.Second, func() bool {
		return e.count(t, `SELECT count(*) FROM explorer.denoms WHERE mintable IS NOT NULL`) > 0
	}, "the denoms set read the settings")
	before := into[dto.Token](t, fetch(t, base+"/api/v1/tokens/"+url.PathEscape(vdlDenom)))
	require.NotNil(t, before.Config)
	assert.Equal(t, "0.5", before.Config.GasSurcharge)

	e.grpc.SetResponse("tokenfactory", "DenomConfig", url.PathEscape(vdlDenom), []byte(
		`{"config":{"communityPool":"","transferFee":"0.020000000000000000","gasSurcharge":"1.500000000000000000"}}`))
	code, out := e.syncState(t, "--job", "denoms", "--id", vdlDenom)
	require.Equal(t, 0, code, out)

	after := into[dto.Token](t, fetch(t, base+"/api/v1/tokens/"+url.PathEscape(vdlDenom)))
	require.NotNil(t, after.Config)
	assert.Nil(t, after.Config.CommunityPool, "the admin dropped the pool")
	assert.Equal(t, "0.02", after.Config.TransferFee)
	assert.Equal(t, "1.5", after.Config.GasSurcharge)
}
