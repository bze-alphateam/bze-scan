// Package chain holds the facts about the BZE chain the transformer needs:
// the bech32 prefix and the addresses of the module accounts.
package chain

import (
	"github.com/cosmos/cosmos-sdk/types/bech32"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// Bech32Prefix is the human-readable part of BZE account addresses.
const Bech32Prefix = "bze"

// BondDenom is the chain's native denom, in base units. Its display unit is
// BZE with six decimals; the chain stores no bank metadata for it.
const (
	BondDenom         = "ubze"
	BondDenomSymbol   = "BZE"
	BondDenomName     = "BeeZee"
	BondDenomExponent = 6
)

// Module account names, as the SDK modules register them.
const (
	FeeCollector = authtypes.FeeCollectorName
	Distribution = "distribution"
	Mint         = "mint"
)

// ModuleAddress returns the bech32 account address of the module account
// name: the first 20 bytes of sha256(name), as authtypes.NewModuleAddress
// derives it.
func ModuleAddress(name string) string {
	addr, err := bech32.ConvertAndEncode(Bech32Prefix, authtypes.NewModuleAddress(name))
	if err != nil {
		// Only an invalid prefix or an over-long payload fail, and both are
		// constants here.
		panic(err)
	}
	return addr
}
