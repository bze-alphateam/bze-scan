package transform_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bze-alphateam/bze-scan/backend/internal/chain"
	"github.com/bze-alphateam/bze-scan/backend/internal/labels"
	"github.com/bze-alphateam/bze-scan/backend/internal/node"
	"github.com/bze-alphateam/bze-scan/backend/internal/transform"
)

// The chain v8.3.0 upgrade block runs its migration in the PreBlocker: its
// events are finalize events without a mode attribute. This block has the
// shapes the upgrade emits (the real one is recorded once the upgrade has
// happened): a queued order refunded with reason upgrade, the retired
// txfeecollector_burner's coins sent to the burner, and the approved
// account migration (rewards withdrawn to the new address, the unbonding
// paid out, the authz grant moved, the balances in one multi-denom send).
const (
	migratedFrom = "bze1z3mkcr2jz424w6m49frgjmy9uhlrx69phqwg3l"
	migratedTo   = "bze16w8swmdq7p6kcrdunlxwly7zw6pytgl0k7a3z0"
	refundOwner  = "bze1fdyfrv09slmg0q9c8hu334h5gdq4aexcgjghpf"
	grantee      = newAdmin
	migratedLP   = "factory/" + migratedFrom + "/uvdl"
)

func bankEvent(typ string, kv ...string) node.Event {
	ev := node.Event{Type: typ}
	for i := 0; i < len(kv); i += 2 {
		ev.Attributes = append(ev.Attributes, node.Attribute{Key: kv[i], Value: kv[i+1]})
	}
	return ev
}

// send is a bank send's events: coin_spent, coin_received, transfer and
// message.
func send(from, to, amount string) []node.Event {
	return []node.Event{
		bankEvent("coin_spent", "spender", from, "amount", amount),
		bankEvent("coin_received", "receiver", to, "amount", amount),
		bankEvent("transfer", "recipient", to, "sender", from, "amount", amount),
		bankEvent("message", "sender", from),
	}
}

func upgradeBlockEvents() []node.Event {
	tradebin := chain.ModuleAddress("tradebin")
	feeBurner := chain.ModuleAddress("txfeecollector_burner")
	burner := chain.ModuleAddress("burner")
	distribution := chain.ModuleAddress(chain.Distribution)
	notBonded := chain.ModuleAddress("not_bonded_tokens_pool")

	var evs []node.Event
	// The refund of a message still queued.
	evs = append(evs, send(tradebin, refundOwner, "500ubze")...)
	evs = append(evs, node.Event{Type: "bze.tradebin.OrderMessageRefundedEvent", Attributes: []node.Attribute{
		attr("order_id", `"000000000000000000000007"`), attr("market_id", `"`+honey+`/ubze"`),
		attr("order_type", `"buy"`), attr("owner", `"`+refundOwner+`"`), attr("amount", `"10"`),
		attr("refund", `"500ubze"`), attr("reason", `"upgrade"`),
	}})
	// The retired burner fee collector empties into the burner.
	evs = append(evs, send(feeBurner, burner, "1200ubze,30"+honey)...)
	// The account migration.
	evs = append(evs, send(distribution, migratedTo, "40254684ubze")...)
	evs = append(evs,
		bankEvent("withdraw_rewards", "amount", "40254684ubze", "validator", "bzevaloper1te6l2dn6d5uzuf0fvwa392gnuauyqjczyxq84h",
			"delegator", migratedFrom),
		bankEvent("withdraw_rewards", "amount", "0stake", "validator", "bzevaloper138xqs3nhepmjs3pk6d9eprkvp78ye9elje4825",
			"delegator", migratedFrom),
	)
	evs = append(evs, send(notBonded, migratedTo, "7000ubze")...)
	evs = append(evs,
		bankEvent("cosmos.authz.v1beta1.EventRevoke", "msg_type_url", `"/cosmos.bank.v1beta1.MsgSend"`,
			"granter", `"`+migratedFrom+`"`, "grantee", `"`+grantee+`"`),
		bankEvent("cosmos.authz.v1beta1.EventGrant", "msg_type_url", `"/cosmos.bank.v1beta1.MsgSend"`,
			"granter", `"`+migratedTo+`"`, "grantee", `"`+grantee+`"`),
		bankEvent("set_withdraw_address", "withdraw_address", migratedTo),
	)
	evs = append(evs, send(migratedFrom, migratedTo, "965105382489"+migratedLP+",10305686071ubze")...)
	// The routine BeginBlock that follows, with its mode.
	evs = append(evs,
		bankEvent("coinbase", "minter", chain.ModuleAddress(chain.Mint), "amount", "428397ubze", "mode", "BeginBlock"),
		bankEvent("mint", "amount", "428397", "inflation", "0.2", "mode", "BeginBlock"),
	)
	return evs
}

func TestV830UpgradeBlockIsIndexed(t *testing.T) {
	in := baseInput()
	in.Block.Txs, in.Results.TxsResults = nil, nil
	in.Results.FinalizeBlockEvents = upgradeBlockEvents()
	tr, hook := transformerWithLog(t)
	ents, err := tr.Transform(in)
	require.NoError(t, err)
	assert.Empty(t, hook.AllEntries(), "no unclassified bze.* event, nothing undecodable")

	// The refund is stored and attributed to its owner.
	var types []string
	for _, e := range ents.BlockEvents {
		types = append(types, e.Type)
	}
	assert.Equal(t, []string{"bze.tradebin.OrderMessageRefundedEvent"}, types)

	// The transfers are recorded one coin a row, and their module accounts
	// are named.
	names := map[string]string{}
	for _, l := range labels.Modules() {
		names[l.Address] = l.Name
	}
	var moved []string
	for _, r := range ents.Transfers {
		assert.Equal(t, transform.BlockTxIndex, r.TxIndex)
		moved = append(moved, label(names, r.Sender)+" > "+label(names, r.Recipient)+" "+r.Amount+" "+r.Denom)
	}
	assert.Equal(t, []string{
		"DEX > " + refundOwner + " 500 ubze",
		"Burner fee collector (retired in v8.3.0) > Burner 1200 ubze",
		"Burner fee collector (retired in v8.3.0) > Burner 30 " + honey,
		"Distribution > " + migratedTo + " 40254684 ubze",
		"Unbonding tokens pool > " + migratedTo + " 7000 ubze",
		migratedFrom + " > " + migratedTo + " 965105382489 " + migratedLP,
		migratedFrom + " > " + migratedTo + " 10305686071 ubze",
	}, moved)

	// Both sides of the migration and the refunded owner have a block row.
	rows := map[string]transform.Activity{}
	for _, a := range ents.Activity {
		assert.Equal(t, transform.BlockTxIndex, a.TxIndex)
		rows[a.Address] = a
	}
	require.Contains(t, rows, refundOwner)
	assert.Equal(t, "dex_refund", rows[refundOwner].Kind)
	require.Contains(t, rows, migratedFrom)
	require.Contains(t, rows, migratedTo)
	from, to := rows[migratedFrom], rows[migratedTo]
	assert.Equal(t, "chain_transfer", from.Kind)
	assert.Equal(t, "sent", from.Category)
	assert.Equal(t, migratedTo, from.Counterparty)
	assert.False(t, from.IsSigner)
	assert.Equal(t, []chain.Coin{{Denom: migratedLP, Amount: "-965105382489"}, {Denom: "ubze", Amount: "-10305686071"}}, from.Deltas)
	assert.Equal(t, "chain_transfer", to.Kind)
	assert.Equal(t, "received", to.Category)
	assert.Equal(t, migratedFrom, to.Counterparty)
	assert.Equal(t, []chain.Coin{{Denom: migratedLP, Amount: "965105382489"}, {Denom: "ubze", Amount: "10345947755"}}, to.Deltas,
		"the balances, the rewards and the unbonding")
	assert.Len(t, rows, 3, "the module accounts have none")
}

func label(names map[string]string, addr string) string {
	if n, ok := names[addr]; ok {
		return n
	}
	return addr
}
