package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

func NewRootCommand(stdout, stderr io.Writer) *cobra.Command {
	collectorURL := defaultCollectorURL()
	cmd := &cobra.Command{
		Use:   "kubectl-memlens",
		Short: "Terminal-first Kubernetes memory inspector",
		Long: "KubeMemLens helps explain why pod or container memory is high by separating cgroup memory " +
			"into RSS/anon, file cache, tmpfs, slab, dirty/writeback, and pressure signals.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.PersistentFlags().StringVar(&collectorURL, "collector-url", collectorURL, "collector base URL")

	cmd.AddCommand(newSampleCommand())
	cmd.AddCommand(newTopCommand(&collectorURL))
	cmd.AddCommand(newExplainCommand(&collectorURL))
	cmd.AddCommand(newTUICommand())

	return cmd
}

func Execute(stdout, stderr io.Writer) error {
	cmd := NewRootCommand(stdout, stderr)
	if err := cmd.Execute(); err != nil {
		return fmt.Errorf("%w", err)
	}

	return nil
}

func defaultCollectorURL() string {
	if value := os.Getenv("MEMLENS_COLLECTOR_URL"); value != "" {
		return value
	}
	return "http://127.0.0.1:18080"
}
