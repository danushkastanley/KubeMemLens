package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newTUICommand() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open the terminal dashboard",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "TUI is planned for v0.2. Use sample top/explain for v0.1.")
			return nil
		},
	}
}
