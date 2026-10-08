// Package classify is the explorer's classification of chain activity: per
// message type URL, the kind of action and the activity category of its signer
// and of the other accounts it names; per block-level event type, the kind,
// the category and the attributes that name accounts.
//
// The tables are the source of truth. `migrate` mirrors them into
// explorer.message_kinds and explorer.block_event_kinds (Mirror) so SQL sees
// the same mapping; the indexer reads these tables, never the mirror. A type
// missing here is "other", never an error, and gets reclassified in place by
// explorer.reclassify_unknown() once an entry is added.
package classify

import "sort"

// Activity categories.
const (
	Sent       = "sent"
	Received   = "received"
	Staking    = "staking"
	DEX        = "dex"
	Governance = "governance"
	Tokens     = "tokens"
	Rewards    = "rewards"
	Burner     = "burner"
	CrossChain = "cross_chain"
	Other      = "other"
	KindOther  = "other"
	KindRelay  = "relay"
	KindGroup  = "group"
)

// MessageKind classifies one message type.
type MessageKind struct {
	TypeURL             string
	Kind                string
	SignerCategory      string
	ParticipantCategory string
	// CounterpartyAttr is "<event type>.<attribute>" naming the counterparty
	// (split at the last dot), or empty.
	CounterpartyAttr string
}

// BlockEventKind classifies one block-level (EndBlock/BeginBlock) event type.
// Only these event types are stored in explorer.block_events.
type BlockEventKind struct {
	EventType string
	Kind      string
	Category  string
	// AddressAttrs are the attributes whose values name accounts.
	AddressAttrs []string
}

// OtherMessage is what Lookup answers for a type URL without an entry.
var OtherMessage = MessageKind{Kind: KindOther, SignerCategory: Other, ParticipantCategory: Other}

func msg(typeURL, kind, signer, participant, counterparty string) MessageKind {
	return MessageKind{TypeURL: typeURL, Kind: kind, SignerCategory: signer, ParticipantCategory: participant, CounterpartyAttr: counterparty}
}

// messages is the message classification, one entry per type URL.
var messages = []MessageKind{
	// Bank, NFT, vesting.
	msg("/cosmos.bank.v1beta1.MsgSend", "send", Sent, Received, "transfer.recipient"),
	msg("/cosmos.bank.v1beta1.MsgMultiSend", "send", Sent, Received, ""),
	msg("/cosmos.nft.v1beta1.MsgSend", "nft_send", Sent, Received, "cosmos.nft.v1beta1.EventSend.receiver"),
	msg("/cosmos.vesting.v1beta1.MsgCreateVestingAccount", "vesting_create", Sent, Received, "transfer.recipient"),
	msg("/cosmos.vesting.v1beta1.MsgCreatePermanentLockedAccount", "vesting_create", Sent, Received, "transfer.recipient"),
	msg("/cosmos.vesting.v1beta1.MsgCreatePeriodicVestingAccount", "vesting_create", Sent, Received, "transfer.recipient"),

	// Staking, distribution, slashing.
	msg("/cosmos.staking.v1beta1.MsgDelegate", "delegate", Staking, Staking, "delegate.validator"),
	msg("/cosmos.staking.v1beta1.MsgUndelegate", "undelegate", Staking, Staking, "unbond.validator"),
	msg("/cosmos.staking.v1beta1.MsgBeginRedelegate", "redelegate", Staking, Staking, "redelegate.destination_validator"),
	msg("/cosmos.staking.v1beta1.MsgCancelUnbondingDelegation", "cancel_unbonding", Staking, Staking, "cancel_unbonding_delegation.validator"),
	msg("/cosmos.staking.v1beta1.MsgCreateValidator", "validator", Staking, Staking, ""),
	msg("/cosmos.staking.v1beta1.MsgEditValidator", "validator", Staking, Staking, ""),
	msg("/cosmos.slashing.v1beta1.MsgUnjail", "validator", Staking, Staking, ""),
	msg("/cosmos.distribution.v1beta1.MsgWithdrawDelegatorReward", "claim_rewards", Staking, Staking, "withdraw_rewards.validator"),
	msg("/cosmos.distribution.v1beta1.MsgWithdrawValidatorCommission", "claim_commission", Staking, Staking, "withdraw_commission.validator"),
	msg("/cosmos.distribution.v1beta1.MsgSetWithdrawAddress", "withdraw_address", Staking, Staking, "set_withdraw_address.withdraw_address"),
	msg("/cosmos.distribution.v1beta1.MsgFundCommunityPool", "community_pool_fund", Other, Other, ""),
	msg("/cosmos.distribution.v1beta1.MsgDepositValidatorRewardsPool", "validator_rewards_deposit", Staking, Staking, ""),

	// Governance.
	msg("/cosmos.gov.v1.MsgSubmitProposal", "proposal", Governance, Governance, ""),
	msg("/cosmos.gov.v1.MsgDeposit", "proposal_deposit", Governance, Governance, ""),
	msg("/cosmos.gov.v1.MsgVote", "vote", Governance, Governance, "proposal_vote.proposal_id"),
	msg("/cosmos.gov.v1.MsgVoteWeighted", "vote", Governance, Governance, "proposal_vote.proposal_id"),
	msg("/cosmos.gov.v1.MsgCancelProposal", "proposal_cancel", Governance, Governance, ""),
	msg("/cosmos.gov.v1beta1.MsgSubmitProposal", "proposal", Governance, Governance, ""),
	msg("/cosmos.gov.v1beta1.MsgDeposit", "proposal_deposit", Governance, Governance, ""),
	msg("/cosmos.gov.v1beta1.MsgVote", "vote", Governance, Governance, "proposal_vote.proposal_id"),
	msg("/cosmos.gov.v1beta1.MsgVoteWeighted", "vote", Governance, Governance, "proposal_vote.proposal_id"),

	// Authz and fee grants.
	msg("/cosmos.authz.v1beta1.MsgExec", "authz_exec", Other, Other, ""),
	msg("/cosmos.authz.v1beta1.MsgGrant", "authz_grant", Other, Other, "cosmos.authz.v1beta1.EventGrant.grantee"),
	msg("/cosmos.authz.v1beta1.MsgRevoke", "authz_revoke", Other, Other, "cosmos.authz.v1beta1.EventRevoke.grantee"),
	msg("/cosmos.feegrant.v1beta1.MsgGrantAllowance", "feegrant_grant", Other, Other, "set_feegrant.grantee"),
	msg("/cosmos.feegrant.v1beta1.MsgRevokeAllowance", "feegrant_revoke", Other, Other, "revoke_feegrant.grantee"),
	msg("/cosmos.feegrant.v1beta1.MsgPruneAllowances", "feegrant_prune", Other, Other, ""),

	// Groups (x/group is wired but unused on BeeZee).
	msg("/cosmos.group.v1.MsgCreateGroup", KindGroup, Other, Other, ""),
	msg("/cosmos.group.v1.MsgCreateGroupPolicy", KindGroup, Other, Other, ""),
	msg("/cosmos.group.v1.MsgCreateGroupWithPolicy", KindGroup, Other, Other, ""),
	msg("/cosmos.group.v1.MsgExec", KindGroup, Other, Other, ""),
	msg("/cosmos.group.v1.MsgLeaveGroup", KindGroup, Other, Other, ""),
	msg("/cosmos.group.v1.MsgSubmitProposal", KindGroup, Other, Other, ""),
	msg("/cosmos.group.v1.MsgUpdateGroupAdmin", KindGroup, Other, Other, ""),
	msg("/cosmos.group.v1.MsgUpdateGroupMembers", KindGroup, Other, Other, ""),
	msg("/cosmos.group.v1.MsgUpdateGroupMetadata", KindGroup, Other, Other, ""),
	msg("/cosmos.group.v1.MsgUpdateGroupPolicyAdmin", KindGroup, Other, Other, ""),
	msg("/cosmos.group.v1.MsgUpdateGroupPolicyDecisionPolicy", KindGroup, Other, Other, ""),
	msg("/cosmos.group.v1.MsgUpdateGroupPolicyMetadata", KindGroup, Other, Other, ""),
	msg("/cosmos.group.v1.MsgVote", KindGroup, Other, Other, ""),
	msg("/cosmos.group.v1.MsgWithdrawProposal", KindGroup, Other, Other, ""),

	msg("/cosmos.evidence.v1beta1.MsgSubmitEvidence", "evidence", Other, Other, ""),

	// IBC: transfers, fees, interchain accounts.
	msg("/ibc.applications.transfer.v1.MsgTransfer", "ibc_out", CrossChain, CrossChain, "ibc_transfer.receiver"),
	msg("/ibc.applications.fee.v1.MsgPayPacketFee", "ibc_fee", CrossChain, CrossChain, ""),
	msg("/ibc.applications.fee.v1.MsgPayPacketFeeAsync", "ibc_fee", CrossChain, CrossChain, ""),
	msg("/ibc.applications.fee.v1.MsgRegisterPayee", KindRelay, Other, Other, ""),
	msg("/ibc.applications.fee.v1.MsgRegisterCounterpartyPayee", KindRelay, Other, Other, ""),
	msg("/ibc.applications.interchain_accounts.controller.v1.MsgRegisterInterchainAccount", "ica", CrossChain, CrossChain, ""),
	msg("/ibc.applications.interchain_accounts.controller.v1.MsgSendTx", "ica", CrossChain, CrossChain, ""),

	// IBC relaying: signed by relayers. A non-signer gets a participant row
	// only when its balance changed (ibc_in, ibc_refund; activity feed story).
	msg("/ibc.core.channel.v1.MsgRecvPacket", KindRelay, Other, CrossChain, "fungible_token_packet.sender"),
	msg("/ibc.core.channel.v1.MsgAcknowledgement", KindRelay, Other, CrossChain, ""),
	msg("/ibc.core.channel.v1.MsgTimeout", KindRelay, Other, CrossChain, ""),
	msg("/ibc.core.channel.v1.MsgTimeoutOnClose", KindRelay, Other, CrossChain, ""),
	msg("/ibc.core.client.v1.MsgUpdateClient", KindRelay, Other, Other, ""),
	msg("/ibc.core.client.v1.MsgCreateClient", KindRelay, Other, Other, ""),
	msg("/ibc.core.client.v1.MsgUpgradeClient", KindRelay, Other, Other, ""),
	msg("/ibc.core.client.v1.MsgSubmitMisbehaviour", KindRelay, Other, Other, ""),
	msg("/ibc.core.connection.v1.MsgConnectionOpenInit", KindRelay, Other, Other, ""),
	msg("/ibc.core.connection.v1.MsgConnectionOpenTry", KindRelay, Other, Other, ""),
	msg("/ibc.core.connection.v1.MsgConnectionOpenAck", KindRelay, Other, Other, ""),
	msg("/ibc.core.connection.v1.MsgConnectionOpenConfirm", KindRelay, Other, Other, ""),
	msg("/ibc.core.channel.v1.MsgChannelOpenInit", KindRelay, Other, Other, ""),
	msg("/ibc.core.channel.v1.MsgChannelOpenTry", KindRelay, Other, Other, ""),
	msg("/ibc.core.channel.v1.MsgChannelOpenAck", KindRelay, Other, Other, ""),
	msg("/ibc.core.channel.v1.MsgChannelOpenConfirm", KindRelay, Other, Other, ""),
	msg("/ibc.core.channel.v1.MsgChannelCloseInit", KindRelay, Other, Other, ""),
	msg("/ibc.core.channel.v1.MsgChannelCloseConfirm", KindRelay, Other, Other, ""),
	msg("/ibc.core.channel.v1.MsgChannelUpgradeInit", KindRelay, Other, Other, ""),
	msg("/ibc.core.channel.v1.MsgChannelUpgradeTry", KindRelay, Other, Other, ""),
	msg("/ibc.core.channel.v1.MsgChannelUpgradeAck", KindRelay, Other, Other, ""),
	msg("/ibc.core.channel.v1.MsgChannelUpgradeConfirm", KindRelay, Other, Other, ""),
	msg("/ibc.core.channel.v1.MsgChannelUpgradeOpen", KindRelay, Other, Other, ""),
	msg("/ibc.core.channel.v1.MsgChannelUpgradeTimeout", KindRelay, Other, Other, ""),
	msg("/ibc.core.channel.v1.MsgChannelUpgradeCancel", KindRelay, Other, Other, ""),
	msg("/ibc.core.channel.v1.MsgPruneAcknowledgements", KindRelay, Other, Other, ""),

	// bze.tradebin
	msg("/bze.tradebin.MsgCreateOrder", "dex_order", DEX, DEX, ""),
	msg("/bze.tradebin.MsgCancelOrder", "dex_cancel", DEX, DEX, ""),
	msg("/bze.tradebin.MsgFillOrders", "dex_fill", DEX, DEX, ""),
	msg("/bze.tradebin.MsgMultiSwap", "dex_swap", DEX, DEX, ""),
	msg("/bze.tradebin.MsgAddLiquidity", "liquidity", DEX, DEX, ""),
	msg("/bze.tradebin.MsgRemoveLiquidity", "liquidity", DEX, DEX, ""),
	msg("/bze.tradebin.MsgCreateLiquidityPool", "dex_pool", DEX, DEX, ""),
	msg("/bze.tradebin.MsgCreateMarket", "dex_market", DEX, DEX, ""),

	// bze.burner
	msg("/bze.burner.MsgFundBurner", "burner_fund", Burner, Burner, ""),
	msg("/bze.burner.MsgJoinRaffle", "raffle_join", Burner, Burner, ""),
	msg("/bze.burner.MsgStartRaffle", "raffle_start", Burner, Burner, ""),
	msg("/bze.burner.MsgMoveIbcLockedCoins", "burner_move", Burner, Burner, ""),

	// bze.tokenfactory
	msg("/bze.tokenfactory.MsgCreateDenom", "token_create", Tokens, Tokens, ""),
	msg("/bze.tokenfactory.MsgMint", "token_mint", Tokens, Tokens, ""),
	msg("/bze.tokenfactory.MsgBurn", "token_burn", Tokens, Tokens, ""),
	msg("/bze.tokenfactory.MsgChangeAdmin", "token_admin", Tokens, Tokens, ""),
	msg("/bze.tokenfactory.MsgSetDenomMetadata", "token_update", Tokens, Tokens, ""),
	msg("/bze.tokenfactory.MsgSetDenomBranding", "token_update", Tokens, Tokens, ""),

	// bze.rewards
	msg("/bze.rewards.MsgJoinStaking", "reward_join", Rewards, Rewards, ""),
	msg("/bze.rewards.MsgExitStaking", "reward_exit", Rewards, Rewards, ""),
	msg("/bze.rewards.MsgClaimStakingRewards", "reward_claim", Rewards, Rewards, ""),
	msg("/bze.rewards.MsgJoinDenomReward", "reward_join", Rewards, Rewards, ""),
	msg("/bze.rewards.MsgExitDenomReward", "reward_exit", Rewards, Rewards, ""),
	msg("/bze.rewards.MsgClaimDenomRewards", "reward_claim", Rewards, Rewards, ""),
	msg("/bze.rewards.MsgCreateStakingReward", "reward_program", Rewards, Rewards, ""),
	msg("/bze.rewards.MsgUpdateStakingReward", "reward_program", Rewards, Rewards, ""),
	msg("/bze.rewards.MsgCreateTradingReward", "reward_program", Rewards, Rewards, ""),
	msg("/bze.rewards.MsgActivateTradingReward", "reward_program", Rewards, Rewards, ""),
	msg("/bze.rewards.MsgDistributeStakingRewards", "reward_program", Rewards, Rewards, ""),
	msg("/bze.rewards.MsgDeleteStakingReward", "reward_program", Rewards, Rewards, ""),
	msg("/bze.rewards.MsgCreateDenomReward", "reward_program", Rewards, Rewards, ""),
	msg("/bze.rewards.MsgCreateDenomRewardSchedule", "reward_program", Rewards, Rewards, ""),
	msg("/bze.rewards.MsgUpdateDenomRewardSchedule", "reward_program", Rewards, Rewards, ""),
	msg("/bze.rewards.MsgDistributeDenomRewards", "reward_program", Rewards, Rewards, ""),

	// bze.cointrunk
	msg("/bze.cointrunk.MsgAddArticle", "article", Other, Received, ""),
	msg("/bze.cointrunk.MsgPayPublisherRespect", "respect", Other, Received, ""),
}

// Unclassified are message types the chain registers that deliberately have
// no entry: signed only by the governance authority (parameter updates,
// upgrades, governance-curated lists) or by nobody on BeeZee (circuit breaker,
// host-side interchain accounts). Lookup answers "other" for them.
var Unclassified = []string{
	"/bze.burner.MsgUpdateParams",
	"/bze.cointrunk.MsgAcceptDomain",
	"/bze.cointrunk.MsgSavePublisher",
	"/bze.cointrunk.MsgUpdateParams",
	"/bze.rewards.MsgUpdateParams",
	"/bze.tokenfactory.MsgUpdateParams",
	"/bze.tradebin.MsgHaltDenoms",
	"/bze.tradebin.MsgUnhaltDenoms",
	"/bze.tradebin.MsgUpdateParams",
	"/bze.txfeecollector.MsgUpdateParams",
	"/cosmos.auth.v1beta1.MsgUpdateParams",
	"/cosmos.bank.v1beta1.MsgSetSendEnabled",
	"/cosmos.bank.v1beta1.MsgUpdateParams",
	"/cosmos.circuit.v1.MsgAuthorizeCircuitBreaker",
	"/cosmos.circuit.v1.MsgResetCircuitBreaker",
	"/cosmos.circuit.v1.MsgTripCircuitBreaker",
	"/cosmos.consensus.v1.MsgUpdateParams",
	"/cosmos.distribution.v1beta1.MsgCommunityPoolSpend",
	"/cosmos.distribution.v1beta1.MsgUpdateParams",
	"/cosmos.gov.v1.MsgExecLegacyContent",
	"/cosmos.gov.v1.MsgUpdateParams",
	"/cosmos.mint.v1beta1.MsgUpdateParams",
	"/cosmos.slashing.v1beta1.MsgUpdateParams",
	"/cosmos.staking.v1beta1.MsgUpdateParams",
	"/cosmos.upgrade.v1beta1.MsgCancelUpgrade",
	"/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade",
	"/ibc.applications.interchain_accounts.controller.v1.MsgUpdateParams",
	"/ibc.applications.interchain_accounts.host.v1.MsgModuleQuerySafe",
	"/ibc.applications.interchain_accounts.host.v1.MsgUpdateParams",
	"/ibc.applications.transfer.v1.MsgUpdateParams",
	"/ibc.core.channel.v1.MsgUpdateParams",
	"/ibc.core.client.v1.MsgIBCSoftwareUpgrade",
	"/ibc.core.client.v1.MsgRecoverClient",
	"/ibc.core.client.v1.MsgUpdateParams",
	"/ibc.core.connection.v1.MsgUpdateParams",
}

func event(eventType, kind, category string, addressAttrs ...string) BlockEventKind {
	if addressAttrs == nil {
		addressAttrs = []string{}
	}
	return BlockEventKind{EventType: eventType, Kind: kind, Category: category, AddressAttrs: addressAttrs}
}

// blockEvents is the block-level event classification. Routine events (mint,
// commission, rewards, proposer_reward, coin_spent, coin_received, coinbase,
// transfer, message, liveness) are absent on purpose: they are not stored.
var blockEvents = []BlockEventKind{
	event("bze.tradebin.OrderExecutedEvent", "dex_fill", DEX, "maker", "taker"),
	event("bze.tradebin.OrderSavedEvent", "dex_order_saved", DEX, "owner"),
	event("bze.tradebin.OrderCanceledEvent", "dex_cancel", DEX, "owner"),
	event("complete_unbonding", "unbonding_completed", Staking, "delegator"),
	event("complete_redelegation", "redelegation_completed", Staking, "delegator"),
	event("bze.rewards.TradingRewardDistributionEvent", "reward_won", Rewards, "winners"),
	event("bze.rewards.StakingRewardDistributionEvent", "reward_program", Rewards),
	event("bze.rewards.DenomRewardDistributionEvent", "reward_program", Rewards),
	event("bze.rewards.DenomRewardScheduleFinishEvent", "reward_program", Rewards),
	event("bze.rewards.TradingRewardExpireEvent", "reward_program", Rewards),
	event("bze.rewards.DenomRewardPrizeCreateEvent", "reward_program", Rewards),
	event("bze.burner.RaffleWinnerEvent", "raffle_won", Burner, "winner"),
	event("bze.burner.RaffleLostEvent", "raffle_lost", Burner, "participant"),
	event("bze.burner.RaffleFinishedEvent", "raffle_finished", Burner),
	event("bze.burner.CoinsBurnedEvent", "burned", Burner),
	event("slash", "slashed", Staking),
	event("active_proposal", "proposal_resolved", Governance),
	event("inactive_proposal", "proposal_dropped", Governance),
	event("bze.epochs.EpochStartEvent", "epoch", Other),
	event("bze.epochs.EpochEndEvent", "epoch", Other),
}

var (
	byTypeURL   = index(messages, func(m MessageKind) string { return m.TypeURL })
	byEventType = index(blockEvents, func(e BlockEventKind) string { return e.EventType })
)

func index[T any](rows []T, key func(T) string) map[string]T {
	out := make(map[string]T, len(rows))
	for _, r := range rows {
		if _, dup := out[key(r)]; dup {
			panic("classify: duplicate entry " + key(r))
		}
		out[key(r)] = r
	}
	return out
}

// Lookup classifies a message type URL; an unknown type gets OtherMessage
// carrying the type URL.
func Lookup(typeURL string) MessageKind {
	if m, ok := byTypeURL[typeURL]; ok {
		return m
	}
	m := OtherMessage
	m.TypeURL = typeURL
	return m
}

// LookupBlockEvent classifies a block-level event type, and reports whether
// the type is stored at all.
func LookupBlockEvent(eventType string) (BlockEventKind, bool) {
	e, ok := byEventType[eventType]
	return e, ok
}

// Messages returns every message entry, sorted by type URL.
func Messages() []MessageKind {
	out := append([]MessageKind(nil), messages...)
	sort.Slice(out, func(i, j int) bool { return out[i].TypeURL < out[j].TypeURL })
	return out
}

// BlockEvents returns every block-level event entry, sorted by event type.
func BlockEvents() []BlockEventKind {
	out := append([]BlockEventKind(nil), blockEvents...)
	sort.Slice(out, func(i, j int) bool { return out[i].EventType < out[j].EventType })
	return out
}
