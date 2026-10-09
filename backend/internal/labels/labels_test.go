package labels_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/labels"
)

// mainnet are the module accounts mainnet's auth module lists
// (/cosmos/auth/v1beta1/module_accounts, 2026-10-09).
var mainnet = map[string]string{
	"bonded_tokens_pool":     "bze1fl48vsnmsdzcv85q5d2q4z5ajdha8yu3gl9ytq",
	"burner":                 "bze1v7uw4xhrcv0vk7qp8jf9lu3hm5d8uu5yjp5qun",
	"burner_black_hole":      "bze1pc5zjcvhx3e8l305zjl72grytfa30r5mdypmw4",
	"burner_raffle":          "bze18hsqalgwlzqavrrkfnxmrjmygwyjy8senx5tgs",
	"distribution":           "bze1jv65s3grqf6v6jl3dp4t6c9t9rk99cd86mghmg",
	"fee_collector":          "bze17xpfvakm2amg962yls6f84z3kell8c5lda0te2",
	"feeibc":                 "bze176rcyfn5k9d0wcxel3kmwvxh0hy3xcweyg0g5n",
	"gov":                    "bze10d07y265gmmuvt4z0w9aw880jnsr700j8xlwyy",
	"interchainaccounts":     "bze1vlthgax23ca9syk7xgaz347xmf4nunef6v6u78",
	"mint":                   "bze1m3h30wlvsf8llruxtpukdvsy0km2kum844tn4h",
	"nft":                    "bze1hr93qzcjspaa32px0qqywlh9hf9a8plgju2a4s",
	"not_bonded_tokens_pool": "bze1tygms3xhhs3yv487phx3dw4a95jn7t7lule4a5",
	"rewards":                "bze1245yut9zht8q4hz39sd0lzqtzkuw5us58k46ua",
	"tokenfactory":           "bze19ejy8n9qsectrf4semdp9cpknflld0j6qy3xlk",
	"tradebin":               "bze18mhtjwczlzqqgvw84uz8lrdv4hqule3jp8allp",
	"transfer":               "bze1yl6hdjhmkf37639730gffanpzndzdpmhnm6z95",
	"txfeecollector":         "bze1k8py7ynzrrnmzqwk2mkz3a6znatmqshdnnsehy",
	"txfeecollector_burner":  "bze1pj73qt0lk9q64vwx4veu53ez6la2409wewcfqq",
	"txfeecollector_cp":      "bze1knfjgvdsv67u2ksejka54ulpfzegr6semkvjn0",
}

const feeCollector = "bze17xpfvakm2amg962yls6f84z3kell8c5lda0te2"

func byModule(t *testing.T) map[string]labels.Label {
	t.Helper()
	out := map[string]labels.Label{}
	for _, l := range labels.Modules() {
		out[l.Module] = l
	}
	return out
}

func TestEveryModuleAccountOfTheChainIsLabelled(t *testing.T) {
	got := map[string]string{}
	for module, l := range byModule(t) {
		got[module] = l.Address
		assert.NotEmpty(t, l.Name, module)
		assert.Equal(t, labels.KindModule, l.Kind)
	}
	assert.Equal(t, mainnet, got)
	assert.Len(t, labels.Modules(), len(mainnet), "one label per module")
}

func TestModuleNames(t *testing.T) {
	m := byModule(t)
	assert.Equal(t, "DEX", m["tradebin"].Name)
	assert.Equal(t, "Factory", m["tokenfactory"].Name)
	assert.Equal(t, "Burner", m["burner"].Name)
	assert.Equal(t, "Fee collector", m["fee_collector"].Name)
}

func TestKnownIsEmptyForNow(t *testing.T) {
	assert.Empty(t, labels.Known())
}

type call struct {
	sql  string
	args []any
}

type recorder struct {
	calls []call
	err   error
}

func (r *recorder) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	r.calls = append(r.calls, call{sql, args})
	return pgconn.CommandTag{}, r.err
}

func TestSeedUpsertsEveryLabelAndDropsStaleSeedRows(t *testing.T) {
	db := &recorder{}
	require.NoError(t, labels.Seed(context.Background(), db))
	require.Len(t, db.calls, 2)

	upsert := db.calls[0]
	assert.Contains(t, upsert.sql, "INSERT INTO explorer.labels")
	assert.Contains(t, upsert.sql, "'seed'")
	assert.Contains(t, upsert.sql, "ON CONFLICT (address) DO UPDATE")
	assert.Contains(t, upsert.sql, "IS DISTINCT FROM", "seeding again rewrites nothing")
	var rows []labels.Label
	require.NoError(t, json.Unmarshal(upsert.args[0].([]byte), &rows))
	assert.Equal(t, labels.Modules(), rows)

	stale := db.calls[1]
	assert.Contains(t, stale.sql, "DELETE FROM explorer.labels WHERE source = 'seed'")
	addrs := stale.args[0].([]string)
	assert.Len(t, addrs, len(rows))
	assert.Contains(t, addrs, feeCollector)
}

func TestSeedIsIdempotent(t *testing.T) {
	first, second := &recorder{}, &recorder{}
	require.NoError(t, labels.Seed(context.Background(), first))
	require.NoError(t, labels.Seed(context.Background(), second))
	assert.Equal(t, first.calls, second.calls, "the same statements with the same rows")
}

func TestSeedFails(t *testing.T) {
	err := labels.Seed(context.Background(), &recorder{err: errors.New("down")})
	assert.ErrorContains(t, err, "labels: down")
}
