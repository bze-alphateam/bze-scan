package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Option configures the root command.
type Option func(*options)

type options struct {
	version string
}

// WithVersion sets what the version command prints: the commit the binary
// was built from.
func WithVersion(v string) Option {
	return func(o *options) { o.version = v }
}

func newVersionCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the commit the binary was built from",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), version)
			return err
		},
	}
}
