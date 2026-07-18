package cli

import (
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/danushkastanley/kube-memlens/internal/buildinfo"
	"github.com/spf13/cobra"
)

func newVersionCommand() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show KubeMemLens build information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			info := buildinfo.Current(runtime.Version(), runtime.GOOS, runtime.GOARCH)
			switch output {
			case "text":
				_, err := fmt.Fprintln(cmd.OutOrStdout(), info.String())
				return err
			case "json":
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(info)
			default:
				return fmt.Errorf("unsupported output %q; expected text or json", output)
			}
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "text", "output format: text or json")
	return cmd
}
