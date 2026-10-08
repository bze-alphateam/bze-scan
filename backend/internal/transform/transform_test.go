package transform

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
)

var update = flag.Bool("update", false, "rewrite the golden files from the transformer's output")

// goldenBlock is every explorer.blocks column the transformer fills, as the
// golden files store it. block_time_ms is the writer's (it reads the previous
// row) and is covered by the acceptance tests.
type goldenBlock struct {
	Height              int64           `json:"height"`
	Time                time.Time       `json:"time"`
	TxCount             int             `json:"tx_count"`
	TxFailedCount       int             `json:"tx_failed_count"`
	Hash                string          `json:"hash"`
	ProposerConsAddress string          `json:"proposer_cons_address"`
	SizeBytes           int             `json:"size_bytes"`
	Minted              *string         `json:"minted"`
	Inflation           *string         `json:"inflation"`
	FeesDistributed     json.RawMessage `json:"fees_distributed"`
	SignaturesCount     int             `json:"signatures_count"`
	SignaturesPowerPct  *string         `json:"signatures_power_pct"`
}

func fetchInput(t *testing.T, n *fakenode.Node, h int64) Input {
	t.Helper()
	c := node.New(n.URL)
	ctx := context.Background()
	b, _, err := c.Block(ctx, h)
	require.NoError(t, err)
	r, _, err := c.BlockResults(ctx, h)
	require.NoError(t, err)
	cm, _, err := c.Commit(ctx, h)
	require.NoError(t, err)
	return Input{Block: b, Results: r, Commit: cm}
}

func TestBlockGolden(t *testing.T) {
	n := fakenode.New(t)
	tr := New()
	for _, h := range n.FixtureHeights() {
		t.Run(fmt.Sprint(h), func(t *testing.T) {
			ents, err := tr.Transform(fetchInput(t, n, h))
			require.NoError(t, err)
			require.Len(t, ents.Blocks, 1)
			b := ents.Blocks[0]

			fees, err := b.FeesDistributedJSON()
			require.NoError(t, err)
			got, err := json.MarshalIndent(goldenBlock{
				Height: b.Height, Time: b.Time, TxCount: b.TxCount, TxFailedCount: b.TxFailedCount,
				Hash: b.Hash, ProposerConsAddress: b.ProposerConsAddress, SizeBytes: b.SizeBytes,
				Minted: b.Minted, Inflation: b.Inflation, FeesDistributed: fees,
				SignaturesCount: b.SignaturesCount, SignaturesPowerPct: b.SignaturesPowerPct,
			}, "", "  ")
			require.NoError(t, err)
			got = append(got, '\n')

			file := filepath.Join("testdata", fmt.Sprintf("block_%d.golden.json", h))
			if *update {
				require.NoError(t, os.MkdirAll("testdata", 0o755))
				require.NoError(t, os.WriteFile(file, got, 0o644))
			}
			want, err := os.ReadFile(file)
			require.NoError(t, err, "run go test ./internal/transform -update to create it")
			assert.Equal(t, string(want), string(got))
		})
	}
}

// The size is the byte length of the "block" object exactly as the node sent it.
func TestBlockSizeIsTheRawBlockObject(t *testing.T) {
	n := fakenode.New(t)
	in := fetchInput(t, n, 24998316)
	ents, err := New().Transform(in)
	require.NoError(t, err)
	assert.Equal(t, len(in.Block.Raw), ents.Blocks[0].SizeBytes)
	assert.Positive(t, ents.Blocks[0].SizeBytes)
}

func baseInput() Input {
	return Input{
		Block:   &node.Block{Height: 10, Time: time.Unix(100, 0), Hash: "AB", Txs: []string{"a", "b"}, Raw: json.RawMessage(`{}`)},
		Results: &node.BlockResults{Height: 10, TxsResults: []node.TxResult{{Code: 0}, {Code: 5}}},
		Commit: &node.Commit{Height: 10, Signatures: []node.CommitSig{
			{BlockIDFlag: node.BlockIDFlagCommit}, {BlockIDFlag: 1}, {BlockIDFlag: 3}, {BlockIDFlag: node.BlockIDFlagCommit},
		}},
	}
}

func transfer(sender, recipient, amount string) node.Event {
	return node.Event{Type: "transfer", Attributes: []node.Attribute{
		{Key: "recipient", Value: recipient}, {Key: "sender", Value: sender}, {Key: "amount", Value: amount},
	}}
}

func TestCountsAndFees(t *testing.T) {
	tr := New()
	in := baseInput()
	in.Results.FinalizeBlockEvents = []node.Event{
		transfer(tr.feeCollector, tr.distribution, "5ubze,7ibc/ABC"),
		transfer(tr.feeCollector, "bze1someoneelse", "100ubze"),
		transfer("bze1someoneelse", tr.distribution, "100ubze"),
		transfer(tr.feeCollector, tr.distribution, "3ubze"),
	}
	ents, err := tr.Transform(in)
	require.NoError(t, err)
	b := ents.Blocks[0]

	assert.Equal(t, 2, b.TxCount)
	assert.Equal(t, 1, b.TxFailedCount)
	assert.Equal(t, 2, b.SignaturesCount)
	assert.Nil(t, b.Minted)
	assert.Nil(t, b.Inflation)
	assert.Nil(t, b.SignaturesPowerPct)
	fees, err := b.FeesDistributedJSON()
	require.NoError(t, err)
	assert.JSONEq(t, `[{"denom":"ibc/ABC","amount":"7"},{"denom":"ubze","amount":"8"}]`, string(fees))
}

func TestNoFeeTransferIsNull(t *testing.T) {
	ents, err := New().Transform(baseInput())
	require.NoError(t, err)
	fees, err := ents.Blocks[0].FeesDistributedJSON()
	require.NoError(t, err)
	assert.Nil(t, fees)
}

func TestRejectsInconsistentInput(t *testing.T) {
	tr := New()
	cases := map[string]func(in *Input){
		"missing commit": func(in *Input) { in.Commit = nil },
		"results height": func(in *Input) { in.Results.Height = 11 },
		"commit height":  func(in *Input) { in.Commit.Height = 9 },
		"result count":   func(in *Input) { in.Results.TxsResults = in.Results.TxsResults[:1] },
		"bad mint amount": func(in *Input) {
			in.Results.FinalizeBlockEvents = []node.Event{{Type: "mint", Attributes: []node.Attribute{{Key: "amount", Value: "1.5"}}}}
		},
		"bad mint inflation": func(in *Input) {
			in.Results.FinalizeBlockEvents = []node.Event{{Type: "mint", Attributes: []node.Attribute{{Key: "inflation", Value: "abc"}}}}
		},
		"bad fee distribution": func(in *Input) {
			in.Results.FinalizeBlockEvents = []node.Event{transfer(tr.feeCollector, tr.distribution, "five ubze")}
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
