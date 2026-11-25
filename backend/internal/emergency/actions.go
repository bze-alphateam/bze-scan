package emergency

import "sort"

// The emergency actions of the index: the messages and the block-level
// events that pull a switch or release it, each named by one action.
const (
	TightenRateLimit = "tighten_rate_limit"
	BlockDenom       = "block_denom"
	UnblockDenom     = "unblock_denom"
	DisableMessages  = "disable_messages"
	EnableMessages   = "enable_messages"
	HaltTrading      = "halt_trading"
	UnhaltTrading    = "unhalt_trading"
	HaltChain        = "halt_chain"
	PauseAutomation  = "pause_automation"
	ResumeAutomation = "resume_automation"
	CloseChannel     = "close_channel"
	ReopenChannel    = "reopen_channel"
	SetGuardian      = "set_guardian"
)

// messageActions maps the type URL of a message to its action.
var messageActions = map[string]string{
	"/bze.guardian.v1.MsgTightenRateLimit":      TightenRateLimit,
	"/bze.guardian.v1.MsgBlockDenom":            BlockDenom,
	"/bze.guardian.v1.MsgUnblockDenom":          UnblockDenom,
	"/bze.guardian.v1.MsgDisableMessages":       DisableMessages,
	"/bze.guardian.v1.MsgHaltTradingDenoms":     HaltTrading,
	"/bze.guardian.v1.MsgHaltChain":             HaltChain,
	"/bze.guardian.v1.MsgPauseAutomation":       PauseAutomation,
	"/bze.guardian.v1.MsgResumeAutomation":      ResumeAutomation,
	"/bze.guardian.v1.MsgCloseChannel":          CloseChannel,
	"/bze.guardian.v1.MsgReopenChannel":         ReopenChannel,
	"/bze.guardian.v1.MsgUpdateParams":          SetGuardian,
	"/cosmos.circuit.v1.MsgResetCircuitBreaker": EnableMessages,
	"/bze.tradebin.MsgHaltDenoms":               HaltTrading,
	"/bze.tradebin.MsgUnhaltDenoms":             UnhaltTrading,
}

// eventActions maps the type of a block-level guardian event (a guardian
// message executed by a governance proposal) to its action.
var eventActions = map[string]string{
	"bze.guardian.v1.TightenRateLimitEvent":    TightenRateLimit,
	"bze.guardian.v1.DenomBlockedEvent":        BlockDenom,
	"bze.guardian.v1.DenomUnblockedEvent":      UnblockDenom,
	"bze.guardian.v1.MessagesDisabledEvent":    DisableMessages,
	"bze.guardian.v1.TradingDenomsHaltedEvent": HaltTrading,
	"bze.guardian.v1.ChainHaltScheduledEvent":  HaltChain,
	"bze.guardian.v1.AutomationPausedEvent":    PauseAutomation,
	"bze.guardian.v1.AutomationResumedEvent":   ResumeAutomation,
	"bze.guardian.v1.ChannelClosedEvent":       CloseChannel,
	"bze.guardian.v1.ChannelReopenedEvent":     ReopenChannel,
}

// MessageTypes are the type URLs of the messages the actions list reads.
func MessageTypes() []string { return keys(messageActions) }

// EventTypes are the types of the block events the actions list reads.
func EventTypes() []string { return keys(eventActions) }

// MessageAction names the action of a message type URL.
func MessageAction(typeURL string) (string, bool) {
	a, ok := messageActions[typeURL]
	return a, ok
}

// EventAction names the action of a block event type.
func EventAction(eventType string) (string, bool) {
	a, ok := eventActions[eventType]
	return a, ok
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// govOnly are the message types only governance can run: they never appear
// as transaction messages (the governance module cannot sign) and their
// events are not stored as block events, so the index knows them only as
// the messages of a passed proposal.
var govOnly = []string{
	"/bze.tradebin.MsgHaltDenoms",
	"/bze.tradebin.MsgUnhaltDenoms",
	"/cosmos.circuit.v1.MsgResetCircuitBreaker",
	"/bze.guardian.v1.MsgUpdateParams",
}

// GovernanceMessageTypes are the type URLs read from the messages of passed
// proposals.
func GovernanceMessageTypes() []string { return append([]string{}, govOnly...) }
