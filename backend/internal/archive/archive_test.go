package archive_test

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/archive"
	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

var update = flag.Bool("golden", false, "rewrite the golden files from the adapter's output")

var (
	codecOnce sync.Once
	codec     *chain.Codec
	codecErr  error
)

func realAdapter(t *testing.T) *archive.Adapter {
	t.Helper()
	codecOnce.Do(func() { codec, codecErr = chain.NewCodec() })
	require.NoError(t, codecErr)
	return archive.New(codec, archive.Mainnet)
}

func fetchInput(t *testing.T, n *fakenode.Node, h int64) transform.Input {
	t.Helper()
	c := node.New(n.URL)
	ctx := context.Background()
	b, _, err := c.Block(ctx, h)
	require.NoError(t, err)
	r, _, err := c.BlockResults(ctx, h)
	require.NoError(t, err)
	cm, _, err := c.Commit(ctx, h)
	require.NoError(t, err)
	return transform.Input{Block: b, Results: r, Commit: cm}
}

func TestMainnetGenerations(t *testing.T) {
	cases := []struct {
		height  int64
		release string
		format  archive.Format
	}{
		{1, "v5.0", archive.FormatLegacy},
		{3_646_699, "v5.0", archive.FormatLegacy},
		{3_646_700, "v5.1.2", archive.FormatLegacy},
		{4_875_460, "v6.0.0", archive.FormatLegacy},
		{9_079_079, "v6.1.0", archive.FormatLegacy},
		{12_723_000, "v7.0.0", archive.FormatLegacy},
		{20_237_799, "v7.2.0", archive.FormatLegacy},
		{20_237_800, "v8.0.0", archive.FormatCurrent},
		{22_551_809, "v8.0.0", archive.FormatCurrent},
		{22_551_810, "v8.1.0", archive.FormatCurrent},
		{23_855_000, "v8.1.1", archive.FormatCurrent},
		{30_000_000, "v8.1.1", archive.FormatCurrent},
	}
	for _, c := range cases {
		g := archive.Mainnet.At(c.height)
		assert.Equal(t, c.release, g.Release, "height %d", c.height)
		assert.Equal(t, c.format, g.Format, "height %d", c.height)
	}
}

func TestTallySetHeight(t *testing.T) {
	cases := []struct{ resolved, set int64 }{
		{3_646_699, 3_646_700},   // v5.0: gov, then staking
		{3_646_700, 3_646_702},   // v5.1.2: staking, then gov
		{17_131_437, 17_131_439}, // v7.2.0, proposal 43
		{20_237_799, 20_237_801}, // the last v7.2.0 block
		{20_237_800, 20_237_801}, // v8.0.0: gov first again
		{23_821_225, 23_821_226}, // v8.1.0, proposal 47
	}
	for _, c := range cases {
		assert.Equal(t, c.set, archive.Mainnet.TallySetHeight(c.resolved), "resolved at %d", c.resolved)
	}
}

func TestGenerationsAreOrdered(t *testing.T) {
	for id, c := range archive.Chains {
		assert.Equal(t, id, c.ID)
		for i := 1; i < len(c.Generations); i++ {
			assert.Less(t, c.Generations[i-1].From, c.Generations[i].From, id)
		}
		assert.Equal(t, int64(1), c.Generations[0].From, id)
	}
}

func TestTestnet3Generations(t *testing.T) {
	cases := []struct {
		height  int64
		release string
		doubled bool
	}{
		{1, "v8.0.2", true},
		{599_999, "v8.0.2", true},
		{600_000, "v8.1.0", false},
		{2_254_400, "v8.1.1", false},
		{3_626_000, "v8.2.0", false},
		{3_757_152, "v8.2.0", false},
	}
	for _, c := range cases {
		g := archive.Testnet3.At(c.height)
		assert.Equal(t, c.release, g.Release, "height %d", c.height)
		assert.Equal(t, archive.FormatCurrent, g.Format, "every bzetestnet-3 height is current, height %d", c.height)
		assert.Equal(t, c.doubled, g.DoubledEvents, "height %d", c.height)
		assert.False(t, g.StakingBeforeGov, "height %d", c.height)
	}
	assert.Equal(t, int64(3_757_153), archive.Testnet3.TallySetHeight(3_757_152), "gov runs first on every testnet release")
}

func TestForChain(t *testing.T) {
	c, err := archive.ForChain("beezee-1")
	require.NoError(t, err)
	assert.Equal(t, archive.Mainnet.ID, c.ID)
	c, err = archive.ForChain("bzetestnet-3")
	require.NoError(t, err)
	assert.Equal(t, archive.Testnet3.ID, c.ID)
	_, err = archive.ForChain("bzetestnet-2")
	require.ErrorIs(t, err, archive.ErrUnknownChain)
}

// Regression (BFE-104): on testnet a low height is v8, not mainnet's legacy
// v5. The adapter used mainnet's table for every chain, so a current-format
// testnet block at 3,757,152 (mainnet's v5.1.2) failed as an unknown legacy
// generation. A current-format block adapted as that testnet height passes.
func TestTestnetHeightIsCurrentFormat(t *testing.T) {
	n := fakenode.New(t)
	codecOnce.Do(func() { codec, codecErr = chain.NewCodec() })
	require.NoError(t, codecErr)
	var h int64
	for _, fh := range n.FixtureHeights() {
		if fh >= 23_855_000 { // a mainnet v8.1.1 block: current format, not doubled
			h = fh
			break
		}
	}
	require.NotZero(t, h, "a current-format fixture")
	const testnetHeight = 3_757_152

	in := fetchInput(t, n, h)
	require.NoError(t, archive.New(codec, archive.Testnet3).Adapt(testnetHeight, &in))

	in = fetchInput(t, n, h)
	err := archive.New(codec, archive.Mainnet).Adapt(testnetHeight, &in)
	require.ErrorIs(t, err, archive.ErrUnknownGeneration, "mainnet's table reads that height as legacy")
}

// Every legacy release line has at least three recorded heights.
func TestEveryLegacyLineHasFixtures(t *testing.T) {
	perLine := map[string]int{}
	for _, h := range fakenode.New(t).FixtureHeights() {
		if g := archive.Mainnet.At(h); g.Format == archive.FormatLegacy {
			perLine[g.Release[:2]]++
		}
	}
	for _, line := range []string{"v5", "v6", "v7"} {
		assert.GreaterOrEqual(t, perLine[line], 3, line)
	}
}

// The normalised results of every legacy fixture height, as golden files.
func TestNormaliseGolden(t *testing.T) {
	n := fakenode.New(t)
	a := realAdapter(t)
	for _, h := range n.FixtureHeights() {
		if archive.Mainnet.At(h).Format != archive.FormatLegacy {
			continue
		}
		t.Run(fmt.Sprint(h), func(t *testing.T) {
			in := fetchInput(t, n, h)
			require.NoError(t, a.Adapt(h, &in))
			got, err := json.MarshalIndent(in.Results, "", "  ")
			require.NoError(t, err)
			got = append(got, '\n')

			file := filepath.Join("testdata", fmt.Sprintf("%d.golden.json", h))
			if *update {
				require.NoError(t, os.MkdirAll("testdata", 0o755))
				require.NoError(t, os.WriteFile(file, got, 0o644))
			}
			want, err := os.ReadFile(file)
			require.NoError(t, err, "run go test ./internal/archive -golden to create it")
			assert.Equal(t, string(want), string(got))
		})
	}
}

// After normalisation a legacy height has the current shape: every event of
// a successful transaction after the ante handler carries a msg_index, and
// no typed event keeps an old package.
func TestNormalisedHeightsHaveTheCurrentShape(t *testing.T) {
	n := fakenode.New(t)
	a := realAdapter(t)
	for _, h := range n.FixtureHeights() {
		if archive.Mainnet.At(h).Format != archive.FormatLegacy {
			continue
		}
		in := fetchInput(t, n, h)
		require.NoError(t, a.Adapt(h, &in))
		for i, res := range in.Results.TxsResults {
			if res.Code != 0 {
				continue
			}
			seen := false
			for _, ev := range res.Events {
				if _, ok := ev.Get("action"); ok && ev.Type == "message" {
					seen = true
				}
				_, indexed := ev.Get("msg_index")
				assert.Equal(t, seen, indexed, "height %d tx %d event %s", h, i, ev.Type)
				assert.NotRegexp(t, `^bze\..*\.v1\.|^bze\.v1\.`, ev.Type)
			}
		}
	}
}

func TestCurrentGenerationIsIdentity(t *testing.T) {
	n := fakenode.New(t)
	a := realAdapter(t)
	checked := 0
	for _, h := range n.FixtureHeights() {
		if g := archive.Mainnet.At(h); g.Format != archive.FormatCurrent || g.DoubledEvents {
			continue
		}
		in := fetchInput(t, n, h)
		before, err := json.Marshal(in)
		require.NoError(t, err)
		require.NoError(t, a.Adapt(h, &in))
		after, err := json.Marshal(in)
		require.NoError(t, err)
		assert.JSONEq(t, string(before), string(after), "height %d", h)
		checked++
	}
	assert.Positive(t, checked)
}

// Only v8.0.x doubles its block events.
func TestOnlyV800DoublesItsEvents(t *testing.T) {
	var doubled []string
	for _, g := range archive.Mainnet.Generations {
		if g.DoubledEvents {
			doubled = append(doubled, g.Release)
		}
	}
	assert.Equal(t, []string{"v8.0.0"}, doubled)
}

// The recorded v8.0.x blocks: every wrapped run of finalize-block events
// appears twice, and the adapter keeps one copy.
func TestV800DoubledRunsAreDropped(t *testing.T) {
	n := fakenode.New(t)
	a := realAdapter(t)
	types := func(h int64) (before, after []string) {
		in := fetchInput(t, n, h)
		for _, ev := range in.Results.FinalizeBlockEvents {
			before = append(before, ev.Type)
		}
		require.NoError(t, a.Adapt(h, &in))
		for _, ev := range in.Results.FinalizeBlockEvents {
			after = append(after, ev.Type)
		}
		return before, after
	}
	count := func(types []string, typ string) int {
		c := 0
		for _, x := range types {
			if x == typ {
				c++
			}
		}
		return c
	}

	t.Run("raffle win: the prize transfer and the event once", func(t *testing.T) {
		before, after := types(20267245)
		assert.Equal(t, 2, count(before, "bze.burner.RaffleWinnerEvent"))
		assert.Equal(t, 1, count(after, "bze.burner.RaffleWinnerEvent"))
		assert.Equal(t, count(before, "transfer")-1, count(after, "transfer"), "the begin-block transfers are not doubled")
		assert.Equal(t, len(before)-5, len(after), "coin_spent, coin_received, transfer, message and the event")
	})
	t.Run("raffle loss", func(t *testing.T) {
		before, after := types(20329219)
		assert.Equal(t, 2, count(before, "bze.burner.RaffleLostEvent"))
		assert.Equal(t, 1, count(after, "bze.burner.RaffleLostEvent"))
	})
	t.Run("raffle clean-up in an epoch hook", func(t *testing.T) {
		before, after := types(20342600)
		assert.Equal(t, 2, count(before, "bze.burner.RaffleFinishedEvent"))
		assert.Equal(t, 1, count(after, "bze.burner.RaffleFinishedEvent"))
		assert.Equal(t, count(before, "bze.epochs.EpochEndEvent"), count(after, "bze.epochs.EpochEndEvent"),
			"the epochs module's own events are not wrapped")
	})
	t.Run("DEX queue: one order saved, two fills", func(t *testing.T) {
		before, after := types(20300028)
		assert.Equal(t, 2, count(before, "bze.tradebin.OrderSavedEvent"))
		assert.Equal(t, 4, count(before, "bze.tradebin.OrderExecutedEvent"))
		assert.Equal(t, 1, count(after, "bze.tradebin.OrderSavedEvent"))
		assert.Equal(t, 2, count(after, "bze.tradebin.OrderExecutedEvent"))
	})
}
