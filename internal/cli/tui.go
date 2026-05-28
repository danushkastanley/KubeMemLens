package cli

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/danushkastanley/kube-memlens/internal/tui"
)

func newTUICommand(collectorOptions collectorOptionsProvider) *cobra.Command {
	var refresh time.Duration
	var namespace string
	var allNamespaces bool
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Open the terminal dashboard",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if cmd.Flags().Changed("namespace") && !cmd.Flags().Changed("all-namespaces") {
				allNamespaces = false
			}
			return tui.Run(cmd.Context(), tui.Options{
				ConnectionOptions: collectorOptions(),
				RefreshInterval:   refresh,
				Namespace:         namespace,
				AllNamespaces:     allNamespaces,
			})
		},
	}
	cmd.Flags().DurationVar(&refresh, "refresh", 5*time.Second, "collector refresh interval")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "namespace to show")
	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", true, "show all namespaces")
	return cmd
}
