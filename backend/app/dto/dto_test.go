package dto_test

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/app/repository"
)

func TestCursorRoundTrip(t *testing.T) {
	for _, keys := range [][]int64{{0}, {25000894}, {math.MaxInt64}, {24999134, 0}, {24999134, 17}} {
		c := dto.EncodeCursor(keys...)
		assert.NotContains(t, c, "=", "no padding")
		got, err := dto.DecodeCursor(c, len(keys))
		require.NoError(t, err, keys)
		assert.Equal(t, keys, got)
	}
}

func TestDecodeCursorRejectsForeignInput(t *testing.T) {
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	cases := map[string]int{
		"not base64!":               1,
		enc("25000894"):             2, // wrong arity
		enc("1:2"):                  1,
		enc("-5"):                   1,
		enc("abc"):                  1,
		enc("+5"):                   1,
		enc("05"):                   1, // not canonical
		enc(""):                     1,
		enc("1:"):                   2,
		enc("99999999999999999999"): 1,
	}
	for c, n := range cases {
		_, err := dto.DecodeCursor(c, n)
		assert.ErrorIs(t, err, dto.ErrInvalidCursor, c)
	}
}

func TestNullColumnsBecomeEmptyOrNull(t *testing.T) {
	tx := dto.NewTx(&repository.Tx{
		TxSummary: repository.TxSummary{Height: 7, Hash: "AB", Time: time.Unix(0, 0).In(time.FixedZone("x", 3600))},
		Messages:  []repository.Message{{TypeURL: "/x.MsgY"}},
	})
	b, err := json.Marshal(tx)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, "1970-01-01T00:00:00Z", got["time"], "times are UTC")
	assert.Equal(t, []any{}, got["msg_types"])
	assert.Equal(t, []any{}, got["fee"])
	assert.Equal(t, []any{}, got["signers"])
	assert.Nil(t, got["memo"])
	assert.Contains(t, got, "memo", "absent values are null, not missing")
	msg := got["messages"].([]any)[0].(map[string]any)
	assert.Nil(t, msg["body"])
	assert.Contains(t, msg, "body")
	assert.Equal(t, []any{}, msg["events"])

	block := dto.NewBlock(&repository.Block{BlockSummary: repository.BlockSummary{Height: 9},
		Transactions: []repository.BlockTx{{TxIndex: 2, Hash: "CD"}}})
	assert.Equal(t, int64(9), block.Transactions[0].Height, "every transaction carries its height")
	b, err = json.Marshal(block)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"fees_distributed":null`)
}
