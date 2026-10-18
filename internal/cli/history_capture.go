package cli

import (
	"fmt"
	"runtime"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/buildinfo"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"github.com/spf13/cobra"
)

func newCaptureTrendsCommand(options collectorOptionsProvider) *cobra.Command {
	var namespace, source, metric, output string
	var window, step time.Duration
	var sensitive, force bool
	cmd := &cobra.Command{Use: "trends <pod|container|workload> <target>", Short: "Capture bounded memory trends and workload change markers with schema 6", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		scope := memoryhistory.Scope(args[0])
		if scope != memoryhistory.Pod && scope != memoryhistory.Container && scope != memoryhistory.Workload {
			return fmt.Errorf("trend capture scope must be pod, container or workload")
		}
		request, err := trendRequest(scope, namespace, args[1])
		if err != nil {
			return err
		}
		if output == "" {
			return fmt.Errorf("--output must not be empty")
		}
		query, err := parseTrendWindow(source, metric, window, step, scope)
		if err != nil {
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
		history, ok := reader.(client.MemoryHistoryContextReader)
		if !ok {
			return fmt.Errorf("trend captures require the authenticated workload change context API")
		}
		value, err := history.MemoryHistoryContext(cmd.Context(), request, query)
		if err != nil {
			return err
		}
		version := buildinfo.Current(runtime.Version(), runtime.GOOS, runtime.GOARCH).String()
		bundle, err := incident.NewHistory(value, version, time.Now().UTC(), sensitive)
		if err != nil {
			return err
		}
		if err := incident.WriteHistory(cmd.OutOrStdout(), output, force, bundle); err != nil {
			return err
		}
		if output != "-" {
			fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s (source=%s; markers=%d; redacted=%t; event history=%s)\n", output, query.Source, len(bundle.Context.Changes.Markers), bundle.Redacted, bundle.Context.Changes.Events)
		}
		return nil
	}}
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes namespace")
	cmd.Flags().StringVar(&source, "source", "local", "history source: local or prometheus; no automatic fallback")
	cmd.Flags().StringVar(&metric, "metric", "", "source-appropriate memory metric")
	cmd.Flags().DurationVar(&window, "window", 0, "bounded history window, up to 168h")
	cmd.Flags().DurationVar(&step, "step", 0, "query step; default bounds the response to 241 points")
	cmd.Flags().StringVarP(&output, "output", "o", "kube-memlens-history.json", "output file, or - for stdout")
	cmd.Flags().BoolVar(&sensitive, "include-sensitive", false, "include raw names, UIDs and container IDs instead of capture-local aliases")
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing output file")
	return cmd
}
