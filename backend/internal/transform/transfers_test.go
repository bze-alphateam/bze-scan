package transform_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// goldenEvents is every transfers and block_events row of one height.
type goldenEvents struct {
	Transfers   []transform.Transfer   `json:"transfers"`
	BlockEvents []transform.BlockEvent `json:"block_events"`
}

func TestTransfersAndBlockEventsGolden(t *testing.T) {
	n := fakenode.New(t)
	tr := realTransformer(t)
	for _, h := range n.FixtureHeights() {
		t.Run(fmt.Sprint(h), func(t *testing.T) {
			ents, err := tr.Transform(fetchInput(t, n, h))
			require.NoError(t, err)
			got, err := json.MarshalIndent(goldenEvents{Transfers: ents.Transfers, BlockEvents: ents.BlockEvents}, "", "  ")
			require.NoError(t, err)
			got = append(got, '\n')

			file := filepath.Join("testdata", fmt.Sprintf("events_%d.golden.json", h))
			if *update {
				require.NoError(t, os.WriteFile(file, got, 0o644))
			}
			want, err := os.ReadFile(file)
			require.NoError(t, err, "run go test ./internal/transform -golden to create it")
			assert.Equal(t, string(want), string(got))
		})
	}
}

var mint = chain.ModuleAddress(chain.Mint)

func intp(i int) *int { return &i }

// Rows that the golden files would only show implicitly.
func TestFixtureTransfers(t *testing.T) {
	n := fakenode.New(t)
	tr := realTransformer(t)
	transformed := func(h int64) *transform.Entities {
		ents, err := tr.Transform(fetchInput(t, n, h))
		require.NoError(t, err)
		return ents
	}
	ofTx := func(ents *transform.Entities, txIndex int) []transform.Transfer {
		var out []transform.Transfer
		for _, r := range ents.Transfers {
			if r.TxIndex == txIndex {
				out = append(out, r)
			}
		}
		return out
	}

	t.Run("send: the fee row, then the message's transfer", func(t *testing.T) {
		ents := transformed(25000894)
		rows := ofTx(ents, 0)
		require.Len(t, rows, 2)
		fee, send := rows[0], rows[1]
		assert.Equal(t, transform.Transfer{
			Height: 25000894, TxIndex: 0, Seq: 0, Kind: transform.TransferKindTransfer,
			Sender: "bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw", Recipient: feeCollector, Denom: "ubze", Amount: "2000",
		}, fee, "the ante handler's fee transfer has no message")
		assert.Equal(t, 1, send.Seq)
		assert.Equal(t, intp(0), send.MsgIndex)
		assert.Equal(t, transform.TransferKindTransfer, send.Kind)
		assert.Equal(t, "bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw", send.Sender)
	})

	t.Run("failed transaction: the fee row only", func(t *testing.T) {
		ents := transformed(24998316)
		for _, tx := range ents.Transactions {
			if tx.Success {
				continue
			}
			rows := ofTx(ents, tx.TxIndex)
			require.Len(t, rows, 1)
			assert.Nil(t, rows[0].MsgIndex)
			assert.Equal(t, feeCollector, rows[0].Recipient)
			assert.Equal(t, tx.FeePayer, rows[0].Sender)
			return
		}
		t.Fatal("no failed transaction in the fixture")
	})

	t.Run("tradebin fills: settlement transfers and OrderExecutedEvent rows", func(t *testing.T) {
		ents := transformed(24999134)
		block := ofTx(ents, transform.BlockTxIndex)
		require.NotEmpty(t, block)
		for i, r := range block {
			assert.Equal(t, i, r.Seq)
			assert.Nil(t, r.MsgIndex)
		}
		var executed int
		for _, e := range ents.BlockEvents {
			if e.Type == "bze.tradebin.OrderExecutedEvent" {
				executed++
				assert.Equal(t, "000000000000000006704522", e.Attrs["id"], "typed values decoded")
			}
		}
		assert.Equal(t, 1, executed)
	})

	t.Run("empty block: no rows of either table", func(t *testing.T) {
		ents := transformed(24998317)
		assert.Empty(t, ents.Transfers)
		assert.Empty(t, ents.BlockEvents)
	})

	t.Run("slash and burn at block level", func(t *testing.T) {
		ents := transformed(24160001)
		var burns int
		for _, r := range ents.Transfers {
			if r.Kind == transform.TransferKindBurn {
				burns++
				assert.Empty(t, r.Recipient)
				assert.NotEmpty(t, r.Sender)
			}
		}
		assert.Equal(t, 1, burns)
		var types []string
		for _, e := range ents.BlockEvents {
			types = append(types, e.Type)
		}
		assert.Contains(t, types, "slash")
	})

	t.Run("routine mint moves are never rows", func(t *testing.T) {
		for _, h := range n.FixtureHeights() {
			for _, r := range transformed(h).Transfers {
				if r.TxIndex != transform.BlockTxIndex {
					continue
				}
				assert.NotEqual(t, mint, r.Recipient, "height %d", h)
				assert.NotEqual(t, mint, r.Sender, "height %d", h)
				assert.False(t, r.Sender == feeCollector && r.Recipient == distribution, "height %d", h)
			}
		}
	})
}

func coinbase(minter, amount string) node.Event {
	return node.Event{Type: "coinbase", Attributes: []node.Attribute{{Key: "minter", Value: minter}, {Key: "amount", Value: amount}}}
}

func withIndex(ev node.Event, j string) node.Event {
	ev.Attributes = append(ev.Attributes, node.Attribute{Key: "msg_index", Value: j})
	return ev
}

func TestBlockTransfersExcludeTheRoutineMovesByAddress(t *testing.T) {
	tr, _ := mockTransformer(mockDecoder{})
	in := baseInput()
	in.Results.FinalizeBlockEvents = []node.Event{
		coinbase(mint, "100ubze"),
		transfer(mint, feeCollector, "100ubze"),
		transfer(feeCollector, distribution, "150ubze"),
		coinbase("bze1someoneelse", "5ufoo"),
		transfer(feeCollector, "bze1someoneelse", "7ubze"),
		transfer(mint, "bze1someoneelse", "8ubze"),
		{Type: "burn", Attributes: []node.Attribute{{Key: "burner", Value: "bze1burner"}, {Key: "amount", Value: "9ubze"}}},
		{Type: "coin_spent", Attributes: []node.Attribute{{Key: "spender", Value: mint}, {Key: "amount", Value: "1ubze"}}},
	}
	ents, err := tr.Transform(in)
	require.NoError(t, err)
	assert.Equal(t, []transform.Transfer{
		{Height: 10, TxIndex: -1, Seq: 0, Kind: "mint", Recipient: "bze1someoneelse", Denom: "ufoo", Amount: "5"},
		{Height: 10, TxIndex: -1, Seq: 1, Kind: "transfer", Sender: feeCollector, Recipient: "bze1someoneelse", Denom: "ubze", Amount: "7"},
		{Height: 10, TxIndex: -1, Seq: 2, Kind: "transfer", Sender: mint, Recipient: "bze1someoneelse", Denom: "ubze", Amount: "8"},
		{Height: 10, TxIndex: -1, Seq: 3, Kind: "burn", Sender: "bze1burner", Denom: "ubze", Amount: "9"},
	}, ents.Transfers)
	b := ents.Blocks[0]
	fees, err := b.FeesDistributedJSON()
	require.NoError(t, err)
	assert.JSONEq(t, `[{"denom":"ubze","amount":"150"}]`, string(fees), "the routine moves still feed the block summary")
}

func TestTxTransfers(t *testing.T) {
	tr, _ := mockTransformer(mockDecoder{})
	in := baseInput()
	in.Results.TxsResults[0].Events = []node.Event{
		transfer("bze1payer", feeCollector, "10ubze"),
		withIndex(node.Event{Type: "message", Attributes: []node.Attribute{{Key: "sender", Value: "bze1payer"}}}, "0"),
		withIndex(transfer("bze1payer", "bze1to", "3ubze,4ufoo"), "0"),
		withIndex(node.Event{Type: "transfer", Attributes: []node.Attribute{{Key: "recipient", Value: "bze1out"}, {Key: "amount", Value: "1ubze"}}}, "1"),
		withIndex(node.Event{Type: "message", Attributes: []node.Attribute{{Key: "sender", Value: "bze1input"}}}, "2"),
		withIndex(node.Event{Type: "transfer", Attributes: []node.Attribute{{Key: "recipient", Value: "bze1out"}, {Key: "amount", Value: "2ubze"}}}, "2"),
		withIndex(coinbase("bze1minter", "5ufactory"), "2"),
		withIndex(node.Event{Type: "burn", Attributes: []node.Attribute{{Key: "burner", Value: "bze1minter"}, {Key: "amount", Value: "6ufactory"}}}, "2"),
	}
	in.Results.TxsResults[1].Events = []node.Event{
		transfer("bze1loser", feeCollector, "11ubze"),
		withIndex(transfer("bze1loser", "bze1to", "3ubze"), "0"),
	}
	ents, err := tr.Transform(in)
	require.NoError(t, err)
	assert.Equal(t, []transform.Transfer{
		{Height: 10, TxIndex: 0, Seq: 0, Kind: "transfer", Sender: "bze1payer", Recipient: feeCollector, Denom: "ubze", Amount: "10"},
		{Height: 10, TxIndex: 0, Seq: 1, MsgIndex: intp(0), Kind: "transfer", Sender: "bze1payer", Recipient: "bze1to", Denom: "ubze", Amount: "3"},
		{Height: 10, TxIndex: 0, Seq: 2, MsgIndex: intp(0), Kind: "transfer", Sender: "bze1payer", Recipient: "bze1to", Denom: "ufoo", Amount: "4"},
		{Height: 10, TxIndex: 0, Seq: 3, MsgIndex: intp(1), Kind: "transfer", Recipient: "bze1out", Denom: "ubze", Amount: "1"},
		{Height: 10, TxIndex: 0, Seq: 4, MsgIndex: intp(2), Kind: "transfer", Sender: "bze1input", Recipient: "bze1out", Denom: "ubze", Amount: "2"},
		{Height: 10, TxIndex: 0, Seq: 5, MsgIndex: intp(2), Kind: "mint", Recipient: "bze1minter", Denom: "ufactory", Amount: "5"},
		{Height: 10, TxIndex: 0, Seq: 6, MsgIndex: intp(2), Kind: "burn", Sender: "bze1minter", Denom: "ufactory", Amount: "6"},
		{Height: 10, TxIndex: 1, Seq: 0, Kind: "transfer", Sender: "bze1loser", Recipient: feeCollector, Denom: "ubze", Amount: "11"},
	}, ents.Transfers, "a multi-coin amount is one row per coin; a failed transaction keeps its fee row only")
}

func TestBlockEventsKeepTheClassifiedTypesOnly(t *testing.T) {
	tr, _ := mockTransformer(mockDecoder{})
	in := baseInput()
	in.Results.FinalizeBlockEvents = []node.Event{
		{Type: "mint", Attributes: []node.Attribute{{Key: "amount", Value: "1"}}},
		{Type: "complete_unbonding", Attributes: []node.Attribute{{Key: "delegator", Value: "bze1d"}, {Key: "amount", Value: "5ubze"}, {Key: "mode", Value: "EndBlock"}}},
		{Type: "liveness", Attributes: []node.Attribute{{Key: "address", Value: "x"}}},
		{Type: "bze.burner.RaffleWinnerEvent", Attributes: []node.Attribute{{Key: "winner", Value: `"bze1w"`}, {Key: "amount", Value: `"12"`}, {Key: "mode", Value: "EndBlock"}}},
		{Type: "bze.unknown.Event", Attributes: []node.Attribute{{Key: "x", Value: `1`}}},
	}
	ents, err := tr.Transform(in)
	require.NoError(t, err)
	assert.Equal(t, []transform.BlockEvent{
		{Height: 10, Seq: 1, Type: "complete_unbonding", Attrs: map[string]any{"delegator": "bze1d", "amount": "5ubze", "mode": "EndBlock"}},
		{Height: 10, Seq: 3, Type: "bze.burner.RaffleWinnerEvent", Attrs: map[string]any{"winner": "bze1w", "amount": "12", "mode": "EndBlock"}},
	}, ents.BlockEvents, "seq is the position in the finalize-block events")
}

func TestTransfersRejectBadAmounts(t *testing.T) {
	tr, _ := mockTransformer(mockDecoder{})
	cases := map[string]func(in *transform.Input){
		"block transfer": func(in *transform.Input) {
			in.Results.FinalizeBlockEvents = []node.Event{transfer("bze1a", "bze1b", "ubze")}
		},
		"tx transfer": func(in *transform.Input) {
			in.Results.TxsResults[0].Events = []node.Event{withIndex(transfer("bze1a", "bze1b", "x"), "0")}
		},
		"msg_index": func(in *transform.Input) {
			in.Results.TxsResults[0].Events = []node.Event{withIndex(transfer("bze1a", "bze1b", "1ubze"), "zero")}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := baseInput()
			mutate(&in)
			_, err := tr.Transform(in)
			assert.Error(t, err)
		})
	}
}
