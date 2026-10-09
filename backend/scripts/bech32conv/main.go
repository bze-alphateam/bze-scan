// Command bech32conv is a helper of scripts/record-grpc-fixtures.sh.
//
//	bech32conv <prefix> <address>   re-encodes the address's bytes under prefix
//	                                (a validator's bze1… owner from its bzevaloper1…)
//	bech32conv consaddr <pubkey>     the bzevalcons1… address of a base64 ed25519
//	                                consensus key
package main

import (
	"encoding/base64"
	"fmt"
	"os"

	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/types/bech32"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "bech32conv:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: bech32conv <prefix> <address> | bech32conv consaddr <base64 ed25519 key>")
	}
	var (
		out string
		err error
	)
	if args[0] == "consaddr" {
		key, derr := base64.StdEncoding.DecodeString(args[1])
		if derr != nil {
			return derr
		}
		pk := ed25519.PubKey{Key: key}
		out, err = bech32.ConvertAndEncode("bzevalcons", pk.Address())
	} else {
		_, bz, derr := bech32.DecodeAndConvert(args[1])
		if derr != nil {
			return derr
		}
		out, err = bech32.ConvertAndEncode(args[0], bz)
	}
	if err != nil {
		return err
	}
	_, err = fmt.Println(out)
	return err
}
