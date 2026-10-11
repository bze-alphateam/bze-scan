//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/app/cli"
	"github.com/bze-alphateam/bze-scan/backend/app/dto"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
	"github.com/bze-alphateam/bze-scan/backend/internal/writer"
)

// The recorded heights of the block-level activity: raffles (v8.0.x, whose
// block events the chain doubled), burns, staking reward distributions,
// epochs and unbonding completions.
const (
	hRaffleWon      = int64(20267245)
	hRaffleLost     = int64(20329219)
	hRaffleFinished = int64(20342600)
	hRedelegated    = int64(24844265)
	hUnbonded       = int64(24871232)
	hWeeklyBurn     = int64(25006237)
	hDailyRewards   = int64(25035768)
	hHourlyEpoch    = int64(25041911)
	// hTradingReward is a synthetic height: mainnet never had a trading
	// reward.
	hTradingReward = int64(25041950)

	raffleWinner = "bze1fdyfrv09slmg0q9c8hu334h5gdq4aexcgjghpf"
	raffleLoser  = "bze1z3mkcr2jz424w6m49frgjmy9uhlrx69phqwg3l"
	unbonder     = "bze1k4flz542uu5qlw7t069lqn7enylku4ge65fd5z"
	redelegator  = "bze12wp3kzu0w0z3zt2n2a4vvhaaset2ytx9xuh9ju"
)

var blockActivityHeights = []int64{
	hRaffleWon, hRaffleLost, hRaffleFinished, hRedelegated, hUnbonded, hWeeklyBurn, hDailyRewards, hHourlyEpoch,
}

// reindexHeights runs `bze-scan reindex --heights` against the env's
// database and fake node: the production path of an old height, archive
// adapter included.
func (e *syncEnv) reindexHeights(t *testing.T, heights []int64) {
	t.Helper()
	t.Chdir(t.TempDir())
	for k, v := range map[string]string{
		"DATABASE_URL": e.url, "NODE_RPC_URL": e.node.URL, "ARCHIVE_RPC_URL": e.node.URL,
		"LOG_LEVEL": "warn", "BACKFILL_WORKERS": "3", "BACKFILL_BATCH": "4", "BACKFILL_RATE_LIMIT": "1000",
	} {
		t.Setenv(k, v)
	}
	list := make([]string, len(heights))
	for i, h := range heights {
		list[i] = fmt.Sprint(h)
	}
	root := cli.NewRootCmd()
	root.SetArgs([]string{"reindex", "--heights", strings.Join(list, ",")})
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	require.NoError(t, root.Execute(), errOut.String())
}

// writeTradingReward writes the synthetic trading-reward height: two
// winners paid by the rewards module.
func (e *syncEnv) writeTradingReward(t *testing.T) {
	t.Helper()
	attr := func(k, v string) node.Attribute { return node.Attribute{Key: k, Value: v} }
	received := func(addr string) node.Event {
		return node.Event{Type: "coin_received", Attributes: []node.Attribute{
			attr("receiver", addr), attr("amount", "500ubze"), attr("mode", "BeginBlock"),
		}}
	}
	in := transform.Input{
		Block: &node.Block{Height: hTradingReward, Time: time.Date(2026, 10, 11, 15, 0, 0, 0, time.UTC),
			Hash: "AB", Raw: json.RawMessage(`{}`)},
		Results: &node.BlockResults{Height: hTradingReward, FinalizeBlockEvents: []node.Event{
			{Type: "bze.rewards.TradingRewardDistributionEvent", Attributes: []node.Attribute{
				attr("reward_id", `"7"`), attr("prize_amount", `"500"`), attr("prize_denom", `"ubze"`),
				attr("winners", `["`+raffleWinner+`","`+unbonder+`"]`), attr("mode", "BeginBlock"),
			}},
			received(raffleWinner), received(unbonder),
		}},
		Commit: &node.Commit{Height: hTradingReward},
	}
	ents, err := newTransformer(t).Transform(in)
	require.NoError(t, err)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, e.url)
	require.NoError(t, err)
	defer pool.Close()
	require.NoError(t, writer.NewLiveWriter(pool).WriteBlock(ctx, ents))
}

// blockLevelRows is every block-level row the heights wrote, with its row
// version (xmin), which changes whenever a row is rewritten.
func (e *syncEnv) blockLevelRows(t *testing.T) []string {
	t.Helper()
	return queryStrings(t, e.db, `
		SELECT 'a ' || xmin || ' ' || row_to_json(a)::text FROM explorer.account_activity a WHERE tx_index = -1
		UNION ALL SELECT 'x ' || xmin || ' ' || row_to_json(x)::text FROM explorer.transfers x WHERE tx_index = -1
		UNION ALL SELECT 'e ' || xmin || ' ' || row_to_json(e)::text FROM explorer.block_events e
		UNION ALL SELECT 'c ' || xmin || ' ' || address || ' ' || activity_count FROM explorer.accounts
		ORDER BY 1`)
}

func TestBlockLevelActivity(t *testing.T) {
	e := newSyncEnv(t)
	e.node.SetStatusHeight(hTradingReward + 1)
	e.reindexHeights(t, blockActivityHeights)
	e.writeTradingReward(t)

	assert.Equal(t, []string{"1"}, queryStrings(t, e.db, `SELECT count(*) FROM explorer.transfers
		WHERE height = $1 AND tx_index = -1`, hRaffleWon), "v8.0.x: the prize transfer once, not as the chain reported it")
	assert.Equal(t, []string{"1"}, queryStrings(t, e.db, `SELECT count(*) FROM explorer.block_events
		WHERE height = $1 AND type = 'bze.burner.RaffleWinnerEvent'`, hRaffleWon))

	before := e.blockLevelRows(t)
	e.reindexHeights(t, blockActivityHeights)
	assert.Equal(t, before, e.blockLevelRows(t), "a reindex rewrites nothing")

	base := e.serveAPI(t)
	type row struct {
		height   int64
		kind     string
		category string
		deltas   string
		events   []string
	}
	rowsOf := func(address string) []row {
		var out []row
		for _, page := range activityPages(t, base, address, "", 25) {
			for _, a := range page {
				assert.Nil(t, a.TxHash, "block-level rows only")
				assert.Nil(t, a.Fee)
				var deltas []string
				for _, d := range a.Deltas {
					deltas = append(deltas, d.Amount+d.Denom)
				}
				var details []struct {
					Type  string         `json:"type"`
					Attrs map[string]any `json:"attrs"`
				}
				require.NoError(t, json.Unmarshal(a.Details, &details))
				var events []string
				for _, d := range details {
					events = append(events, d.Type)
					named := fmt.Sprint(d.Attrs)
					assert.Contains(t, named, address, "every detail names the address")
				}
				out = append(out, row{a.Height, a.Kind, a.Category, strings.Join(deltas, ","), events})
			}
		}
		return out
	}

	assert.Equal(t, []row{
		{hTradingReward, "reward_won", "rewards", "500ubze", []string{"bze.rewards.TradingRewardDistributionEvent"}},
		{hRaffleWon, "raffle_won", "burner", "13100000000ubze", []string{"bze.burner.RaffleWinnerEvent"}},
	}, rowsOf(raffleWinner))
	assert.Equal(t, []row{
		{hRaffleLost, "raffle_lost", "burner", "", []string{"bze.burner.RaffleLostEvent"}},
	}, rowsOf(raffleLoser))
	assert.Equal(t, []row{
		{hTradingReward, "reward_won", "rewards", "500ubze", []string{"bze.rewards.TradingRewardDistributionEvent"}},
		{hUnbonded, "unbonding_completed", "staking", "10000000ubze", []string{"complete_unbonding"}},
	}, rowsOf(unbonder))
	assert.Equal(t, []row{
		{hRedelegated, "redelegation_completed", "staking", "", []string{"complete_redelegation"}},
	}, rowsOf(redelegator))

	// The global list by type, newest first, three at a time.
	var got []string
	url := base + "/block-events?type=bze.epochs.EpochEndEvent&limit=3"
	for next := url; ; {
		r := fetch(t, next)
		require.Equal(t, http.StatusOK, r.status, string(r.body))
		assert.Equal(t, "no-store", r.header.Get("Cache-Control"))
		page := into[dto.List[dto.ChainEvent]](t, r)
		for _, ev := range page.Items {
			var attrs struct{ Identifier string }
			require.NoError(t, json.Unmarshal(ev.Attrs, &attrs))
			assert.Equal(t, "epoch", ev.Kind)
			assert.False(t, ev.Time.IsZero())
			got = append(got, fmt.Sprintf("%d %d %s", ev.Height, ev.Seq, attrs.Identifier))
		}
		if page.NextCursor == nil {
			break
		}
		require.Len(t, page.Items, 3, "only the last page is short")
		next = url + "&cursor=" + *page.NextCursor
	}
	assert.Equal(t, []string{
		"25041911 55 hour",
		"25035768 58 hour", "25035768 56 day",
		"25006237 60 week", "25006237 58 hour", "25006237 56 day",
		"20342600 71 hour",
	}, got)

	burns := into[dto.List[dto.ChainEvent]](t, fetch(t, base+"/block-events?type=bze.burner.CoinsBurnedEvent"))
	require.Len(t, burns.Items, 1)
	assert.Equal(t, hWeeklyBurn, burns.Items[0].Height)
	assert.Equal(t, "burner", burns.Items[0].Category)

	all := into[dto.List[dto.ChainEvent]](t, fetch(t, base+"/block-events?limit=1"))
	require.Len(t, all.Items, 1)
	assert.Equal(t, hTradingReward, all.Items[0].Height, "no type: every stored event, newest first")

	assert.Equal(t, http.StatusBadRequest, fetch(t, base+"/block-events?type=transfer").status)
}
