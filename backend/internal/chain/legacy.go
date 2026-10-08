package chain

import (
	"bytes"
	"fmt"
	"maps"
	"slices"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
)

// legacyTypeURLs maps the type URL a BZE message carried before v8.0.0
// (mainnet height 20,237,800, where every module moved to an unversioned
// proto package) to its current type URL. Every pair was checked to be wire
// compatible: the old message's fields are a subset of the current one's,
// same numbers and same types (renames such as marketId → market_id do not
// change the wire format). Old messages without a current type (the scavenge
// module, removed in v8) are not here: they keep their old type URL and are
// stored without a body.
var legacyTypeURLs = map[string]string{
	"/bze.burner.v1.MsgFundBurner":  "/bze.burner.MsgFundBurner",
	"/bze.burner.v1.MsgStartRaffle": "/bze.burner.MsgStartRaffle",
	"/bze.burner.v1.MsgJoinRaffle":  "/bze.burner.MsgJoinRaffle",

	"/bze.cointrunk.v1.MsgAddArticle":          "/bze.cointrunk.MsgAddArticle",
	"/bze.cointrunk.v1.MsgPayPublisherRespect": "/bze.cointrunk.MsgPayPublisherRespect",

	"/bze.v1.rewards.MsgCreateStakingReward":      "/bze.rewards.MsgCreateStakingReward",
	"/bze.v1.rewards.MsgUpdateStakingReward":      "/bze.rewards.MsgUpdateStakingReward",
	"/bze.v1.rewards.MsgCreateTradingReward":      "/bze.rewards.MsgCreateTradingReward",
	"/bze.v1.rewards.MsgJoinStaking":              "/bze.rewards.MsgJoinStaking",
	"/bze.v1.rewards.MsgExitStaking":              "/bze.rewards.MsgExitStaking",
	"/bze.v1.rewards.MsgClaimStakingRewards":      "/bze.rewards.MsgClaimStakingRewards",
	"/bze.v1.rewards.MsgDistributeStakingRewards": "/bze.rewards.MsgDistributeStakingRewards",

	"/bze.tokenfactory.v1.MsgCreateDenom":      "/bze.tokenfactory.MsgCreateDenom",
	"/bze.tokenfactory.v1.MsgMint":             "/bze.tokenfactory.MsgMint",
	"/bze.tokenfactory.v1.MsgBurn":             "/bze.tokenfactory.MsgBurn",
	"/bze.tokenfactory.v1.MsgChangeAdmin":      "/bze.tokenfactory.MsgChangeAdmin",
	"/bze.tokenfactory.v1.MsgSetDenomMetadata": "/bze.tokenfactory.MsgSetDenomMetadata",

	"/bze.tradebin.v1.MsgCreateMarket": "/bze.tradebin.MsgCreateMarket",
	"/bze.tradebin.v1.MsgCreateOrder":  "/bze.tradebin.MsgCreateOrder",
	"/bze.tradebin.v1.MsgCancelOrder":  "/bze.tradebin.MsgCancelOrder",
	"/bze.tradebin.v1.MsgFillOrders":   "/bze.tradebin.MsgFillOrders",
}

// CanonicalTypeURL returns the current type URL of a message type URL: the
// mapped one for a pre-v8 BZE message, the URL itself otherwise.
func CanonicalTypeURL(typeURL string) string {
	if c, ok := legacyTypeURLs[typeURL]; ok {
		return c
	}
	return typeURL
}

// LegacyTypeURLs lists the pre-v8 BZE message type URLs the codec decodes
// into their current types, sorted.
func LegacyTypeURLs() []string {
	return slices.Sorted(maps.Keys(legacyTypeURLs))
}

// registerLegacyTypeURLs makes registry resolve every legacy type URL to the
// Go type of its current one, so a pre-v8 transaction decodes into today's
// messages and TypeURL reports the current URL.
func registerLegacyTypeURLs(registry codectypes.InterfaceRegistry) error {
	// The SDK's registry has the method, its interface does not expose it.
	custom, ok := registry.(interface {
		RegisterCustomTypeURL(iface any, typeURL string, impl gogoproto.Message)
	})
	if !ok {
		return fmt.Errorf("legacy type URLs: registry %T cannot register custom type URLs", registry)
	}
	for _, old := range LegacyTypeURLs() {
		impl, err := registry.Resolve(legacyTypeURLs[old])
		if err != nil {
			return fmt.Errorf("legacy type URL %s: %w", old, err)
		}
		custom.RegisterCustomTypeURL((*sdk.Msg)(nil), old, impl)
	}
	return nil
}

// canonicalNestedTypes rewrites the "@type" of nested legacy messages in a
// proto JSON document to their current type URL. The exact key-value token
// cannot occur inside a JSON string, where its quotes would be escaped.
func canonicalNestedTypes(doc []byte) []byte {
	if !bytes.Contains(doc, []byte(`"@type":"/bze.`)) {
		return doc
	}
	for _, old := range LegacyTypeURLs() {
		doc = bytes.ReplaceAll(doc, []byte(`"@type":"`+old+`"`), []byte(`"@type":"`+legacyTypeURLs[old]+`"`))
	}
	return doc
}
