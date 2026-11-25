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
	evs = append(evs, send(distribution, migratedTo, "25000000ubze")...)
	evs = append(evs,
		bankEvent("withdraw_rewards", "amount", "25000000ubze", "validator", "bzevaloper1first",
			"delegator", migratedFrom),
		bankEvent("withdraw_rewards", "amount", "0stake", "validator", "bzevaloper1second",
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
	evs = append(evs, send(migratedFrom, migratedTo, "123456789000"+migratedLP+",7000000000ubze")...)
	// The routine BeginBlock that follows, with its mode.
	evs = append(evs,
		bankEvent("coinbase", "minter", chain.ModuleAddress(chain.Mint), "amount", "400000ubze", "mode", "BeginBlock"),
		bankEvent("mint", "amount", "400000", "inflation", "0.2", "mode", "BeginBlock"),
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
		"Distribution > " + migratedTo + " 25000000 ubze",
		"Unbonding tokens pool > " + migratedTo + " 7000 ubze",
		migratedFrom + " > " + migratedTo + " 123456789000 " + migratedLP,
		migratedFrom + " > " + migratedTo + " 7000000000 ubze",
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
	assert.Equal(t, []chain.Coin{{Denom: migratedLP, Amount: "-123456789000"}, {Denom: "ubze", Amount: "-7000000000"}}, from.Deltas)
	assert.Equal(t, "chain_transfer", to.Kind)
	assert.Equal(t, "received", to.Category)
	assert.Equal(t, migratedFrom, to.Counterparty)
	assert.Equal(t, []chain.Coin{{Denom: migratedLP, Amount: "123456789000"}, {Denom: "ubze", Amount: "7025007000"}}, to.Deltas,
		"the balances, the rewards and the unbonding")
	assert.Len(t, rows, 3, "the module accounts have none")
}

func label(names map[string]string, addr string) string {
	if n, ok := names[addr]; ok {
		return n
	}
	return addr
}

// The rule behind chain_transfer rows: a block-level transfer sent by a
// module account names nobody; one sent by any other account, labelled or
// not, gives both sides a row.
func TestBlockTransferFromAModuleAccountGivesNoRow(t *testing.T) {
	in := baseInput()
	in.Block.Txs, in.Results.TxsResults = nil, nil
	in.Results.FinalizeBlockEvents = send(chain.ModuleAddress(chain.Distribution), migratedTo, "5ubze")
	tr, _ := transformerWithLog(t)
	ents, err := tr.Transform(in)
	require.NoError(t, err)
	assert.Empty(t, ents.Activity)
}

func TestBlockTransferFromAnUnlabelledAccountGivesBothSidesARow(t *testing.T) {
	in := baseInput()
	in.Block.Txs, in.Results.TxsResults = nil, nil
	in.Results.FinalizeBlockEvents = send(refundOwner, migratedTo, "5ubze")
	tr, _ := transformerWithLog(t)
	ents, err := tr.Transform(in)
	require.NoError(t, err)
	require.Len(t, ents.Activity, 2)
	category := map[string]string{}
	for _, a := range ents.Activity {
		assert.Equal(t, "chain_transfer", a.Kind)
		assert.Equal(t, transform.BlockTxIndex, a.TxIndex)
		category[a.Address] = a.Category
	}
	assert.Equal(t, map[string]string{refundOwner: "sent", migratedTo: "received"}, category)
}

// The recipient of such a transfer gets no row when it is a module account.
func TestBlockTransferToAModuleAccountGivesTheSenderARowOnly(t *testing.T) {
	in := baseInput()
	in.Block.Txs, in.Results.TxsResults = nil, nil
	in.Results.FinalizeBlockEvents = send(refundOwner, chain.ModuleAddress("burner"), "5ubze")
	tr, _ := transformerWithLog(t)
	ents, err := tr.Transform(in)
	require.NoError(t, err)
	require.Len(t, ents.Activity, 1)
	assert.Equal(t, refundOwner, ents.Activity[0].Address)
	assert.Equal(t, "chain_transfer", ents.Activity[0].Kind)
	assert.Equal(t, chain.ModuleAddress("burner"), ents.Activity[0].Counterparty)
}

// An address a classified block event already names keeps that row: one row
// per address and block.
func TestBlockTransferKeepsTheRowOfAClassifiedEvent(t *testing.T) {
	in := baseInput()
	in.Block.Txs, in.Results.TxsResults = nil, nil
	in.Results.FinalizeBlockEvents = append(send(refundOwner, migratedTo, "5ubze"), node.Event{
		Type: "bze.tradebin.OrderMessageRefundedEvent", Attributes: []node.Attribute{
			attr("order_id", `"7"`), attr("owner", `"`+refundOwner+`"`), attr("reason", `"upgrade"`),
		}})
	tr, _ := transformerWithLog(t)
	ents, err := tr.Transform(in)
	require.NoError(t, err)
	kinds := map[string]string{}
	for _, a := range ents.Activity {
		_, dup := kinds[a.Address]
		assert.False(t, dup, "one row for %s", a.Address)
		kinds[a.Address] = a.Kind
	}
	assert.Equal(t, map[string]string{refundOwner: "dex_refund", migratedTo: "chain_transfer"}, kinds)
}

// A sender that sends to several accounts has no single counterparty.
func TestBlockTransfersToSeveralAccountsLeaveNoCounterparty(t *testing.T) {
	in := baseInput()
	in.Block.Txs, in.Results.TxsResults = nil, nil
	in.Results.FinalizeBlockEvents = append(send(migratedFrom, migratedTo, "5ubze"), send(migratedFrom, refundOwner, "3ubze")...)
	tr, _ := transformerWithLog(t)
	ents, err := tr.Transform(in)
	require.NoError(t, err)
	parties := map[string]string{}
	for _, a := range ents.Activity {
		parties[a.Address] = a.Counterparty
	}
	assert.Equal(t, map[string]string{migratedFrom: "", migratedTo: migratedFrom, refundOwner: migratedFrom}, parties)
}
