package dto_test

import (
	"encoding/base64"
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
	}, nil, nil)
	assert.Equal(t, []string{}, tok.Markets, "an empty list, never null")
	assert.Equal(t, []dto.TokenEvent{}, tok.Events)
	assert.JSONEq(t, `null`, string(tok.Metadata))
	assert.Equal(t, time.UTC, tok.UpdatedAt.Location())
	assert.Nil(t, tok.CreatorLabel)
}
