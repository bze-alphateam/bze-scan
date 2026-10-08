// Command bze-scan is the BZE block explorer backend; the commands live in
// app/cli.
package main

import (
	"os"

	"github.com/bze-alphateam/bze-scan/backend/app/cli"
)

func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
