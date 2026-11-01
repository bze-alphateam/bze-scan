// Package assets builds the asset list of /api/v1/registry/assets: every
// denom the bank holds a supply of, merged with the chain registry cache,
// the IBC channels and the curation file, as the BZE dapps build it on
// every client today. The list is held in memory, rebuilt from the tables
// after the denoms, channels and chain registry sets write, and served as
// pre-rendered JSON with a content-hash version.
package assets

// Kinds of assets.
const (
	KindNative  = "native"
	KindFactory = "factory"
	KindIBC     = "ibc"
	KindLP      = "lp"
)

// BZECircleLogo is the native coin's logo, as the dapps force it (a path
// on the dapp's own host).
const BZECircleLogo = "/images/bze_alternative_512x512.png"

// LPDecimals is a pool share's display exponent in the dapps.
const LPDecimals = 12

// Asset is one entry of the list. Nothing in it changes with blocks (no
// supply): its version moves only when one of these fields does.
type Asset struct {
	Denom           string  `json:"denom"`
	Kind            string  `json:"kind"`
	Name            string  `json:"name"`
	Ticker          string  `json:"ticker"`
	Decimals        int     `json:"decimals"`
	Logo            *string `json:"logo"`
	Description     *string `json:"description"`
	Verified        bool    `json:"verified"`
	Stable          bool    `json:"stable"`
	Excluded        bool    `json:"excluded"`
	Halted          bool    `json:"halted"`
	DepositAllowed  bool    `json:"deposit_allowed"`
	WithdrawAllowed bool    `json:"withdraw_allowed"`
	Notice          *Notice `json:"notice"`
	// IBC is an IBC denom's; nil for the others.
	IBC *IBC `json:"ibc"`
	// LP is a pool share's assets; nil for the others and for a share
	// whose pool is unknown.
	LP *LP `json:"lp"`
	// Routes are the native and factory denoms' ways out; nil for the
	// others, empty without channels.
	Routes  []Route `json:"routes"`
	Version string  `json:"version"`
}

// UnknownChain is the pretty name of the counterparty of an unconfirmed
// channel.
const UnknownChain = "Unknown chain"

// ChainInfo describes a counterparty chain from the registry cache; only
// ChainID is set for a chain the registry does not know (ChainName,
// Logo and Bech32Prefix null).
type ChainInfo struct {
	ChainName    *string  `json:"chain_name"`
	ChainID      string   `json:"chain_id"`
	PrettyName   string   `json:"pretty_name"`
	Logo         *string  `json:"logo"`
	Bech32Prefix *string  `json:"bech32_prefix"`
	REST         []string `json:"rest"`
	RPC          []string `json:"rpc"`
}

// IBC is where an IBC denom came from.
type IBC struct {
	// ChannelID is the BZE channel of the first hop.
	ChannelID    string       `json:"channel_id"`
	Counterparty Counterparty `json:"counterparty"`
	// Origin is nil when the registry cache cannot name the home chain.
	Origin *Origin `json:"origin"`
	// ChannelConfirmed is whether the first hop's counterparty was
	// confirmed to be the chain its light client claims. Without it the
	// denom gets the unknown treatment: nothing the counterparty declares is
	// trusted.
	ChannelConfirmed bool `json:"channel_confirmed"`
}

// Counterparty is the chain at the other end of the BZE channel.
type Counterparty struct {
	ChainInfo
	// ChannelID is the counterparty's end of the channel.
	ChannelID string `json:"channel_id"`
	// BaseDenom is the denom on the counterparty: an ibc/ voucher itself
	// for a multi-hop denom.
	BaseDenom string `json:"base_denom"`
}

// Origin is the chain the denom is native to.
type Origin struct {
	ChainName string `json:"chain_name"`
	BaseDenom string `json:"base_denom"`
	Hops      int    `json:"hops"`
}

// LP is a pool share's two assets.
type LP struct {
	Base  string `json:"base"`
	Quote string `json:"quote"`
}

// Route is a way to send a native or factory denom out of BZE: the
// canonical channel to a chain the registry knows, open.
type Route struct {
	ChainInfo
	ChannelID             string `json:"channel_id"`
	CounterpartyChannelID string `json:"counterparty_channel_id"`
	// CounterpartyDenom is the ICS-20 voucher the denom becomes there.
	CounterpartyDenom string `json:"counterparty_denom"`
}
