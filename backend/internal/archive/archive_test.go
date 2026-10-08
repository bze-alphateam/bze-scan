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
	return archive.New(codec)
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

func TestGenerationAt(t *testing.T) {
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
		{23_855_000, "v8.1.1", archive.FormatCurrent},
		{30_000_000, "v8.1.1", archive.FormatCurrent},
	}
	for _, c := range cases {
		g := archive.GenerationAt(c.height)
		assert.Equal(t, c.release, g.Release, "height %d", c.height)
		assert.Equal(t, c.format, g.Format, "height %d", c.height)
	}
}

func TestGenerationsAreOrdered(t *testing.T) {
	for i := 1; i < len(archive.Generations); i++ {
		assert.Less(t, archive.Generations[i-1].From, archive.Generations[i].From)
	}
	assert.Equal(t, int64(1), archive.Generations[0].From)
}

// Every legacy release line has at least three recorded heights.
func TestEveryLegacyLineHasFixtures(t *testing.T) {
	perLine := map[string]int{}
	for _, h := range fakenode.New(t).FixtureHeights() {
		if g := archive.GenerationAt(h); g.Format == archive.FormatLegacy {
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
		if archive.GenerationAt(h).Format != archive.FormatLegacy {
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
		if archive.GenerationAt(h).Format != archive.FormatLegacy {
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
		if archive.GenerationAt(h).Format != archive.FormatCurrent {
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
