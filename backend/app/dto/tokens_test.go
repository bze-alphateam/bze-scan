package dto_test

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

func TestTokenCursorRoundTrips(t *testing.T) {
	k := repository.TokenKey{KindRank: 2, SortKey: "uusdc é", Denom: "ibc/6490"}
	got, err := dto.DecodeTokenCursor(dto.EncodeTokenCursor(k))
	require.NoError(t, err)
	assert.Equal(t, &k, got)

	for _, bad := range []string{"", "!!", base64.RawURLEncoding.EncodeToString([]byte(`{"r":1}`)),
		base64.RawURLEncoding.EncodeToString([]byte(`{"r":-1,"d":"x"}`)), dto.EncodeCursor(1, 2)} {
		_, err := dto.DecodeTokenCursor(bad)
		assert.ErrorIs(t, err, dto.ErrInvalidCursor, bad)
	}
}

func TestEventCursorKeepsBlockLevelRows(t *testing.T) {
	k := repository.EventKey{Height: 7, TxIndex: -1, Seq: 3}
	got, err := dto.DecodeEventCursor(dto.EncodeEventCursor(k))
	require.NoError(t, err)
	assert.Equal(t, &k, got)
	_, err = dto.DecodeEventCursor(dto.EncodeCursor(7, 3))
	assert.ErrorIs(t, err, dto.ErrInvalidCursor)
}

func TestNewTokenDefaults(t *testing.T) {
	tok := dto.NewToken(&repository.Token{
		TokenSummary: repository.TokenSummary{Denom: "ibc/X", Kind: "ibc"},
		UpdatedAt:    time.Date(2026, 10, 9, 12, 0, 0, 0, time.FixedZone("x", 3600)),
	}, nil, nil, repository.TransferVolume{Count: 2, Amount: "15"})
	assert.Equal(t, []string{}, tok.Markets, "an empty list, never null")
	assert.Equal(t, dto.TransferVolume{Count: 2, Amount: "15"}, tok.Transfers24h)
	assert.Equal(t, []dto.TokenEvent{}, tok.Events)
	assert.JSONEq(t, `null`, string(tok.Metadata))
	assert.Equal(t, time.UTC, tok.UpdatedAt.Location())
	assert.Nil(t, tok.CreatorLabel)
}

func TestNewTokenSummaryCarriesTheFirstSightInUTC(t *testing.T) {
	created := time.Date(2026, 10, 9, 14, 0, 0, 0, time.FixedZone("x", 3600))
	tok := dto.NewToken(&repository.Token{
		TokenSummary: repository.TokenSummary{Denom: "factory/bze1x/uhoney", Kind: "factory", CreatedTime: &created},
	}, nil, nil, repository.TransferVolume{Amount: "0"})
	require.NotNil(t, tok.CreatedTime)
	assert.Equal(t, time.Date(2026, 10, 9, 13, 0, 0, 0, time.UTC), *tok.CreatedTime)
	assert.Nil(t, dto.NewTokenSummary(repository.TokenSummary{Denom: "ubze", Kind: "native"}).CreatedTime)
}

func TestNewTokenSummaryCountsTheIBCHops(t *testing.T) {
	hops := func(kind string, path *string) *int {
		return dto.NewTokenSummary(repository.TokenSummary{Denom: "x", Kind: kind, IBCPath: path}).IBCHops
	}
	one, two := "transfer/channel-13", "transfer/channel-11/transfer/channel-220"
	bad, empty := "transfer/channel-11/transfer", ""
	require.NotNil(t, hops("ibc", &one))
	assert.Equal(t, 1, *hops("ibc", &one))
	assert.Equal(t, 2, *hops("ibc", &two))
	assert.Nil(t, hops("ibc", nil), "not traced yet")
	assert.Nil(t, hops("ibc", &bad), "not port/channel pairs")
	assert.Nil(t, hops("ibc", &empty))
	assert.Nil(t, hops("factory", &one), "only IBC denoms")
}

func TestNewTokenSummaryCarriesTheViaChainAndTheLPPair(t *testing.T) {
	via, name, logo := "cosmoshub-4", "Cosmos Hub", "https://x/hub.png"
	sum := dto.NewTokenSummary(repository.TokenSummary{Denom: "ibc/AB", Kind: "ibc",
		ViaChainID: &via, ViaChainName: &name, ViaChainLogo: &logo})
	assert.Equal(t, &dto.OriginChain{ChainID: via, Name: &name, LogoURL: &logo}, sum.ViaChain)
	assert.Nil(t, sum.LPPair)
	assert.Nil(t, dto.NewTokenSummary(repository.TokenSummary{Denom: "ibc/CD", Kind: "ibc"}).ViaChain, "one hop")

	base, quote := "factory/bze1x/uvdl", "ubze"
	lp := dto.NewTokenSummary(repository.TokenSummary{Denom: "ulp/AB", Kind: "lp", LPBase: &base, LPQuote: &quote})
	assert.Equal(t, &dto.LPPair{Base: base, Quote: quote}, lp.LPPair)
	assert.Nil(t, lp.IBCHops)
	assert.Nil(t, dto.NewTokenSummary(repository.TokenSummary{Denom: "ulp/CD", Kind: "lp"}).LPPair, "pair unknown")
	assert.Nil(t, dto.NewTokenSummary(repository.TokenSummary{Denom: "x", Kind: "factory", LPBase: &base, LPQuote: &quote}).LPPair)

	raw, err := json.Marshal(dto.NewTokenSummary(repository.TokenSummary{Denom: "ubze", Kind: "native"}))
	require.NoError(t, err)
	for _, key := range []string{`"ibc_hops":null`, `"via_chain":null`, `"lp_pair":null`} {
		assert.Contains(t, string(raw), key, "always present")
	}
}
