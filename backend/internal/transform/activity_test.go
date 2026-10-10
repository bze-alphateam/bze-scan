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
	"github.com/bze-alphateam/bze-scan/backend/internal/testutil/fakenode"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

func TestActivityGolden(t *testing.T) {
	n := fakenode.New(t)
	tr := realTransformer(t)
	for _, h := range n.FixtureHeights() {
		t.Run(fmt.Sprint(h), func(t *testing.T) {
			ents, err := tr.Transform(fetchInput(t, n, h))
			require.NoError(t, err)
			rows := ents.Activity
			if rows == nil {
				rows = []transform.Activity{}
			}
			got, err := json.MarshalIndent(rows, "", "  ")
			require.NoError(t, err)
			got = append(got, '\n')

			file := filepath.Join("testdata", fmt.Sprintf("activity_%d.golden.json", h))
			if *update {
				require.NoError(t, os.WriteFile(file, got, 0o644))
			}
			want, err := os.ReadFile(file)
			require.NoError(t, err, "run go test ./internal/transform -golden to create it")
			assert.Equal(t, string(want), string(got))
		})
	}
}

// Rows the acceptance criteria name, from the recorded fixtures.
func TestFixtureActivity(t *testing.T) {
	n := fakenode.New(t)
	tr := realTransformer(t)
	activity := func(h int64) []transform.Activity {
		ents, err := tr.Transform(fetchInput(t, n, h))
		require.NoError(t, err)
		return ents.Activity
	}
	find := func(rows []transform.Activity, address string, txIndex int) *transform.Activity {
		for i := range rows {
			if rows[i].Address == address && rows[i].TxIndex == txIndex {
				return &rows[i]
			}
		}
		return nil
	}
	ofTx := func(rows []transform.Activity, txIndex int) []transform.Activity {
		var out []transform.Activity
		for _, r := range rows {
			if r.TxIndex == txIndex {
				out = append(out, r)
			}
		}
		return out
	}
	const (
		sender    = "bze18uf09nx6tnyaalrruegljgwgfz88vyeq5k9zhw"
		recipient = "bze1xrw00qx3laxg7dkwx0aymxyqtwr9xsuf3k3pa7"
	)

	t.Run("send: the sender's sent row with the fee, the recipient's received row", func(t *testing.T) {
		rows := ofTx(activity(25000894), 0)
		require.Len(t, rows, 2, "the fee collector is a module account: no row")
		s, r := find(rows, sender, 0), find(rows, recipient, 0)
		require.NotNil(t, s)
		require.NotNil(t, r)
		assert.Equal(t, transform.Activity{
			Address: sender, Height: 25000894, TxIndex: 0, Time: s.Time, Category: "sent", Kind: "send",
			IsSigner: true, Success: true, Counterparty: recipient,
			Deltas:   []chain.Coin{{Denom: "ubze", Amount: "-333611764700"}},
			MsgTypes: []string{"/cosmos.bank.v1beta1.MsgSend"},
		}, *s, "333611762700 sent plus the 2000 fee")
		assert.Equal(t, "received", r.Category)
		assert.Equal(t, "send", r.Kind)
		assert.False(t, r.IsSigner)
		assert.Equal(t, sender, r.Counterparty, "the recipient's counterparty is the sender")
		assert.Equal(t, []chain.Coin{{Denom: "ubze", Amount: "333611762700"}}, r.Deltas)
	})

	t.Run("multi-message: one row per address, every message type", func(t *testing.T) {
		rows := activity(25000439)
		require.Len(t, rows, 1)
		assert.Equal(t, "claim_rewards", rows[0].Kind, "the first message classifies")
		assert.Equal(t, []string{
			"/cosmos.distribution.v1beta1.MsgWithdrawDelegatorReward",
			"/cosmos.distribution.v1beta1.MsgWithdrawValidatorCommission",
		}, rows[0].MsgTypes)
		assert.Equal(t, "bzevaloper19fgph876c3rqxrn6xk5ch6wd73r3g05wzpsht0", rows[0].Counterparty)

		rows = activity(3000081)
		require.Len(t, rows, 1, "three claims, one row")
		assert.Empty(t, rows[0].Counterparty, "rewards from three validators: no single counterparty")
	})

	t.Run("failed transaction: fee_only for the fee payer", func(t *testing.T) {
		rows := ofTx(activity(24998316), 2)
		require.Len(t, rows, 1)
		r := rows[0]
		assert.Equal(t, "fee_only", r.Kind)
		assert.Equal(t, "other", r.Category)
		assert.False(t, r.Success)
		assert.True(t, r.IsSigner)
		assert.Equal(t, []chain.Coin{{Denom: "ubze", Amount: "-15664"}}, r.Deltas)
		assert.Equal(t, []string{"/bze.tradebin.MsgCreateOrder"}, r.MsgTypes, "decoded from the raw transaction")
	})

	t.Run("relay: only the receiver's ibc_in row besides the relayer", func(t *testing.T) {
		rows := ofTx(activity(24999209), 0)
		require.Len(t, rows, 2, "the escrow account gets no row")
		in := find(rows, "bze1mjpllwcpca0t2dmnr3wez2ugkr5l07lf62a32y", 0)
		require.NotNil(t, in)
		assert.Equal(t, "ibc_in", in.Kind)
		assert.Equal(t, "cross_chain", in.Category)
		assert.Empty(t, in.Counterparty, "the sender is on the other chain, the relayer is no counterparty")
		relayer := find(rows, "bze1wf3f5kyrkmpnjy7xxg4mhqscchykfazl22quam", 0)
		require.NotNil(t, relayer)
		assert.Equal(t, "relay", relayer.Kind)
		assert.True(t, relayer.IsSigner)

		// An acknowledgement changes no participant's balance: the relayer only.
		assert.Len(t, ofTx(activity(24999209), 1), 1)
	})

	t.Run("timeout: the refunded sender gets ibc_refund", func(t *testing.T) {
		r := find(activity(20237220), "bze1mjpllwcpca0t2dmnr3wez2ugkr5l07lf62a32y", 1)
		require.NotNil(t, r)
		assert.Equal(t, "ibc_refund", r.Kind)
		assert.Equal(t, "cross_chain", r.Category)
		assert.Equal(t, []chain.Coin{{Denom: "ubze", Amount: "21830924671"}}, r.Deltas)
	})

	t.Run("module accounts never get rows", func(t *testing.T) {
		for _, h := range n.FixtureHeights() {
			for _, r := range activity(h) {
				assert.NotContains(t, []string{feeCollector, distribution, mint}, r.Address, "height %d", h)
			}
		}
	})

	t.Run("block-level fills: maker and taker rows with their events", func(t *testing.T) {
		rows := ofTx(activity(20234375), transform.BlockTxIndex)
		require.Len(t, rows, 2)
		for _, r := range rows {
			assert.Equal(t, "dex_fill", r.Kind)
			assert.Equal(t, "dex", r.Category)
			assert.False(t, r.IsSigner)
			assert.True(t, r.Success)
			assert.Empty(t, r.MsgTypes)
			assert.NotEmpty(t, r.Deltas)
			require.NotEmpty(t, r.Details)
			for _, d := range r.Details {
				assert.Equal(t, "bze.tradebin.OrderExecutedEvent", d.Type)
				assert.Contains(t, []any{d.Attrs["maker"], d.Attrs["taker"]}, r.Address, "every event names the address")
			}
		}
	})

	t.Run("counterparty per entry type", func(t *testing.T) {
		for _, c := range []struct {
			name    string
			height  int64
			txIndex int
			address string
			want    string
		}{
			{"delegate: the validator", 3000275, 0, "bze1s7m86a4z8gqmlr4exmnlkdjz23vweet70u4g2p", "bzevaloper1s7m86a4z8gqmlr4exmnlkdjz23vweet7hc2r77"},
			{"vote: a proposal id is no account", 20122191, 0, "bze1cgtrksac0pjj53lym42uaxrjsumtql29zxv2py", ""},
			{"ibc out: the receiver is on the other chain", 4002550, 0, "bze19l9wsymdh3mp7munzrlk8rs03ttnu2uafdmd0e", ""},
			{"authz grantee: the one signer", 20237220, 0, "bze1uvcwvurgfvhyhlgvg6py724vyvevxhke53kvps", "bze1e762e7chsgz3aurtelcz948j8gscfr0qj2wuwk"},
		} {
			t.Run(c.name, func(t *testing.T) {
				r := find(activity(c.height), c.address, c.txIndex)
				require.NotNil(t, r)
				assert.Equal(t, c.want, r.Counterparty)
			})
		}
	})
}
