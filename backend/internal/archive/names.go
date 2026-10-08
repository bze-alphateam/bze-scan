package archive

// legacyActions maps the message.action of a BZE message handled by the
// legacy router before v8.0.0 (its Msg.Type()) to the type URLs the decoded
// message may carry. Taken from the Type() of every message in the release
// tags v5.0.0 to v7.2.0. The scavenge messages have no current type: they
// keep the type URL they were sent with, under either package the module
// ever had.
var legacyActions = map[string][]string{
	"fund_burner":  {"/bze.burner.MsgFundBurner"},
	"start_raffle": {"/bze.burner.MsgStartRaffle"},
	"join_raffle":  {"/bze.burner.MsgJoinRaffle"},

	"add_article":           {"/bze.cointrunk.MsgAddArticle"},
	"pay_publisher_respect": {"/bze.cointrunk.MsgPayPublisherRespect"},

	"create_staking_reward":      {"/bze.rewards.MsgCreateStakingReward"},
	"update_staking_reward":      {"/bze.rewards.MsgUpdateStakingReward"},
	"create_trading_reward":      {"/bze.rewards.MsgCreateTradingReward"},
	"join_staking":               {"/bze.rewards.MsgJoinStaking"},
	"exit_staking":               {"/bze.rewards.MsgExitStaking"},
	"claim_staking_rewards":      {"/bze.rewards.MsgClaimStakingRewards"},
	"distribute_staking_rewards": {"/bze.rewards.MsgDistributeStakingRewards"},

	"create_denom":       {"/bze.tokenfactory.MsgCreateDenom"},
	"mint":               {"/bze.tokenfactory.MsgMint"},
	"burn":               {"/bze.tokenfactory.MsgBurn"},
	"change_admin":       {"/bze.tokenfactory.MsgChangeAdmin"},
	"set_denom_metadata": {"/bze.tokenfactory.MsgSetDenomMetadata"},

	"create_market": {"/bze.tradebin.MsgCreateMarket"},
	"create_order":  {"/bze.tradebin.MsgCreateOrder"},
	"cancel_order":  {"/bze.tradebin.MsgCancelOrder"},
	"fill_orders":   {"/bze.tradebin.MsgFillOrders"},

	"SubmitScavenge": {"/bze.scavenge.MsgSubmitScavenge", "/bzedgev5.scavenge.MsgSubmitScavenge"},
	"CommitSolution": {"/bze.scavenge.MsgCommitSolution", "/bzedgev5.scavenge.MsgCommitSolution"},
	"RevealSolution": {"/bze.scavenge.MsgRevealSolution", "/bzedgev5.scavenge.MsgRevealSolution"},
}

// legacyEventTypes maps every typed event the chain emitted before v8.0.0
// to its current type. Taken from the proto files of the release tags v6.0.0
// to v7.2.0 (v5 emitted no typed events); every one still exists under the
// current name with the same field names.
var legacyEventTypes = map[string]string{
	"bze.burner.v1.CoinsBurnedEvent":    "bze.burner.CoinsBurnedEvent",
	"bze.burner.v1.FundBurnerEvent":     "bze.burner.FundBurnerEvent",
	"bze.burner.v1.RaffleFinishedEvent": "bze.burner.RaffleFinishedEvent",
	"bze.burner.v1.RaffleLostEvent":     "bze.burner.RaffleLostEvent",
	"bze.burner.v1.RaffleWinnerEvent":   "bze.burner.RaffleWinnerEvent",

	"bze.cointrunk.v1.AcceptedDomainAddedEvent":   "bze.cointrunk.AcceptedDomainAddedEvent",
	"bze.cointrunk.v1.AcceptedDomainUpdatedEvent": "bze.cointrunk.AcceptedDomainUpdatedEvent",
	"bze.cointrunk.v1.ArticleAddedEvent":          "bze.cointrunk.ArticleAddedEvent",
	"bze.cointrunk.v1.PublisherAddedEvent":        "bze.cointrunk.PublisherAddedEvent",
	"bze.cointrunk.v1.PublisherRespectPaidEvent":  "bze.cointrunk.PublisherRespectPaidEvent",
	"bze.cointrunk.v1.PublisherUpdatedEvent":      "bze.cointrunk.PublisherUpdatedEvent",

	"bze.epochs.v1.EpochEndEvent":   "bze.epochs.EpochEndEvent",
	"bze.epochs.v1.EpochStartEvent": "bze.epochs.EpochStartEvent",

	"bze.rewards.v1.StakingRewardClaimEvent":        "bze.rewards.StakingRewardClaimEvent",
	"bze.rewards.v1.StakingRewardCreateEvent":       "bze.rewards.StakingRewardCreateEvent",
	"bze.rewards.v1.StakingRewardDistributionEvent": "bze.rewards.StakingRewardDistributionEvent",
	"bze.rewards.v1.StakingRewardExitEvent":         "bze.rewards.StakingRewardExitEvent",
	"bze.rewards.v1.StakingRewardFinishEvent":       "bze.rewards.StakingRewardFinishEvent",
	"bze.rewards.v1.StakingRewardJoinEvent":         "bze.rewards.StakingRewardJoinEvent",
	"bze.rewards.v1.StakingRewardUpdateEvent":       "bze.rewards.StakingRewardUpdateEvent",
	"bze.rewards.v1.TradingRewardActivationEvent":   "bze.rewards.TradingRewardActivationEvent",
	"bze.rewards.v1.TradingRewardCreateEvent":       "bze.rewards.TradingRewardCreateEvent",
	"bze.rewards.v1.TradingRewardDistributionEvent": "bze.rewards.TradingRewardDistributionEvent",
	"bze.rewards.v1.TradingRewardExpireEvent":       "bze.rewards.TradingRewardExpireEvent",

	"bze.tradebin.v1.MarketCreatedEvent":      "bze.tradebin.MarketCreatedEvent",
	"bze.tradebin.v1.OrderCanceledEvent":      "bze.tradebin.OrderCanceledEvent",
	"bze.tradebin.v1.OrderCancelMessageEvent": "bze.tradebin.OrderCancelMessageEvent",
	"bze.tradebin.v1.OrderCreateMessageEvent": "bze.tradebin.OrderCreateMessageEvent",
	"bze.tradebin.v1.OrderExecutedEvent":      "bze.tradebin.OrderExecutedEvent",
	"bze.tradebin.v1.OrderSavedEvent":         "bze.tradebin.OrderSavedEvent",
}

// Type URLs of the messages whose events a legacy height lacks facts for.
var (
	voteTypes = []string{
		"/cosmos.gov.v1beta1.MsgVote", "/cosmos.gov.v1beta1.MsgVoteWeighted",
		"/cosmos.gov.v1.MsgVote", "/cosmos.gov.v1.MsgVoteWeighted",
	}
	depositTypes = []string{"/cosmos.gov.v1beta1.MsgDeposit", "/cosmos.gov.v1.MsgDeposit"}
	submitTypes  = []string{"/cosmos.gov.v1beta1.MsgSubmitProposal", "/cosmos.gov.v1.MsgSubmitProposal"}
	transferType = "/ibc.applications.transfer.v1.MsgTransfer"
	execType     = "/cosmos.authz.v1beta1.MsgExec"
)
