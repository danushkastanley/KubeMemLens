package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"github.com/danushkastanley/kube-memlens/internal/replicaview"
	"github.com/spf13/cobra"
)

func newReplicasCommand(options collectorOptionsProvider) *cobra.Command {
	root := &cobra.Command{Use: "replicas", Short: "Compare current memory evidence across authorised workload replicas"}
	for _, scope := range []memoryhistory.Scope{memoryhistory.Pod, memoryhistory.Workload} {
		root.AddCommand(newReplicaScopeCommand(options, scope))
	}
	return root
}

func newReplicaScopeCommand(options collectorOptionsProvider, scope memoryhistory.Scope) *cobra.Command {
	var namespace, output string
	target := "<name>"
	if scope == memoryhistory.Workload {
		target = "<kind>/<name>"
	}
	cmd := &cobra.Command{Use: string(scope) + " " + target, Short: "Compare " + string(scope) + " replicas without changing diagnosis severity", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		request, err := trendRequest(scope, namespace, args[0])
		if err != nil {
			return err
		}
		if err := validateNodeOutput(output); err != nil {
			return err
		}
		opts, err := withReadScope(options(), namespace, false)
		if err != nil {
			return err
		}
		reader, description, err := client.NewSnapshotReader(cmd.Context(), opts)
		if err != nil {
			return collectorUnavailableError(opts, description, err)
		}
		replicas, ok := reader.(client.ReplicaReader)
		if !ok {
			return fmt.Errorf("replica comparisons require the authenticated replica API")
		}
		report, err := replicas.ReplicaBaseline(cmd.Context(), request)
		if err != nil {
			return err
		}
		if output != "text" {
			return writeNodeDocument(cmd.OutOrStdout(), output, report)
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), strings.Join(replicaview.Lines(report, time.Now().UTC()), "\n"))
		return err
	}}
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes namespace")
	cmd.Flags().StringVarP(&output, "output", "o", "text", "output format: text, json or yaml; authorised peer names are included")
	return cmd
}
