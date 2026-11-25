package controller_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

func (f *fakeTokens) TokenSettings(context.Context, string) (*repository.TokenSettings, error) {
	return f.settings, f.err
}

func TestTokenCarriesItsFactorySettings(t *testing.T) {
	off, pool := false, "bze1pool"
	f := &fakeTokens{token: honeyToken(), events: tokenEvents(1), settings: &repository.TokenSettings{
		Mintable: &off,
		Config:   &repository.TokenConfig{CommunityPool: &pool, TransferFee: "0.010000000000000000", GasSurcharge: "0.5"},
	}}
	rec := get(t, tokenServer(f), "/api/v1/tokens/"+url.PathEscape(honeyDenom))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	tok := decode[dto.Token](t, rec)
	require.NotNil(t, tok.Mintable)
	assert.False(t, *tok.Mintable)
	require.NotNil(t, tok.Config)
	assert.Equal(t, "bze1pool", *tok.Config.CommunityPool)
	assert.Equal(t, "0.010000000000000000", tok.Config.TransferFee)
	assert.Equal(t, "0.5", tok.Config.GasSurcharge)
}

func TestTokenSettingsAreNullWhenUnknown(t *testing.T) {
	f := &fakeTokens{token: honeyToken(), events: tokenEvents(1), settings: &repository.TokenSettings{}}
	rec := get(t, tokenServer(f), "/api/v1/tokens/"+url.PathEscape(honeyDenom))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"mintable":null`)
	assert.Contains(t, rec.Body.String(), `"config":null`)
}
