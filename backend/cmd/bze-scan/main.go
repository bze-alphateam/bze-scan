// Command bze-scan is the BZE block explorer backend; the commands live in
// app/cli.
package main

import (
	"os"

	"github.com/bze-alphateam/bze-scan/backend/app/cli"
)

func main() {
	os.Exit(cli.ExitCode(cli.NewRootCmd().Execute()))
}
