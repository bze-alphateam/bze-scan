// Command bze-scan is the BZE block explorer backend; the commands live in
// app/cli.
package main

import (
	"os"

	"github.com/bze-alphateam/bze-scan/backend/app/cli"
)

// version is the commit the binary was built from, set at build time with
// -ldflags "-X main.version=<sha>" (docker/backend.Dockerfile does).
var version = "dev"

func main() {
	os.Exit(cli.ExitCode(cli.NewRootCmd(cli.WithVersion(version)).Execute()))
}
