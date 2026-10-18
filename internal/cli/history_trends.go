package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/historyview"
	"github.com/danushkastanley/kube-memlens/internal/kube"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"github.com/spf13/cobra"
)

func newHistoryTrendsCommand(options collectorOptionsProvider) *cobra.Command {
	root := &cobra.Command{Use: "trends", Short: "Compare source-labelled local or Prometheus memory trends"}
	for _, scope := range []memoryhistory.Scope{memoryhistory.Pod, memoryhistory.Container, memoryhistory.Workload, memoryhistory.Node} {
		root.AddCommand(newTrendScopeCommand(options, scope))
	}
	return root
}

func newTrendScopeCommand(options collectorOptionsProvider, scope memoryhistory.Scope) *cobra.Command {
	var namespace, source, metric, output string
	var window, step time.Duration
	var markers bool
	target := "<name>"
	if scope == memoryhistory.Container {
		target = "<pod>/<container>"
	}
	if scope == memoryhistory.Workload {
		target = "<kind>/<name>"
	}
	cmd := &cobra.Command{Use: string(scope) + " " + target, Short: "Read bounded " + string(scope) + " memory trends", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		request, err := trendRequest(scope, namespace, args[0])
		if err != nil {
			return err
		}
		if err := validateNodeOutput(output); err != nil {
			return err
		}
		query, err := parseTrendWindow(source, metric, window, step, scope)
		if err != nil {
			return err
		}
		opts := options()
		if scope != memoryhistory.Node {
			opts, err = withReadScope(opts, namespace, false)
			if err != nil {
				return err
			}
		}
		reader, description, err := client.NewSnapshotReader(cmd.Context(), opts)
		if err != nil {
			return collectorUnavailableError(opts, description, err)
		}
		history, ok := reader.(client.MemoryHistoryReader)
		if !ok {
			return fmt.Errorf("memory trends require the authenticated history API")
		}
		if markers {
			contextReader, ok := reader.(client.MemoryHistoryContextReader)
			if !ok || scope == memoryhistory.Node {
				return fmt.Errorf("workload change markers require a Pod, container or workload context reader")
			}
			result, err := contextReader.MemoryHistoryContext(cmd.Context(), request, query)
			if err != nil {
				return err
			}
			if output != "text" {
				return writeNodeDocument(cmd.OutOrStdout(), output, result)
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), strings.Join(historyview.ContextLines(result, time.Now().UTC(), 100), "\n"))
			return err
		}
		r, err := history.MemoryHistory(cmd.Context(), request, query)
		if err != nil {
			return err
		}
		if output != "text" {
			return writeNodeDocument(cmd.OutOrStdout(), output, r)
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), strings.Join(historyview.Lines(r, time.Now().UTC(), 100), "\n"))
		return err
	}}
	if scope != memoryhistory.Node {
		cmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes namespace")
	}
	if scope != memoryhistory.Node {
		cmd.Flags().BoolVar(&markers, "markers", false, "include authorised workload change markers and event-history caveats")
	}
	cmd.Flags().StringVar(&source, "source", "local", "history source: local or prometheus; no automatic fallback")
	cmd.Flags().StringVar(&metric, "metric", "", "metric: cgroup-charge, working-set, or rss (source-appropriate default)")
	cmd.Flags().DurationVar(&window, "window", 0, "history window, up to 168h; default 15m local or 24h Prometheus")
	cmd.Flags().DurationVar(&step, "step", 0, "query grid step; default bounds the response to 241 points")
	cmd.Flags().StringVarP(&output, "output", "o", "text", "output format: text, json, or yaml; authorised names are included")
	return cmd
}

func trendRequest(scope memoryhistory.Scope, namespace, target string) (memoryhistory.Request, error) {
	r := memoryhistory.Request{Scope: scope, Namespace: namespace, Name: target}
	switch scope {
	case memoryhistory.Node:
		r.Namespace = ""
	case memoryhistory.Container, memoryhistory.Workload:
		parts := strings.Split(target, "/")
		if len(parts) != 2 {
			return r, fmt.Errorf("target must contain exactly two slash-separated names")
		}
		if scope == memoryhistory.Container {
			r.Name, r.Container = parts[0], parts[1]
		} else {
			kind, ok := kube.CanonicalVolumeWorkloadKind(parts[0])
			if !ok {
				return r, fmt.Errorf("unsupported workload kind")
			}
			r.Name, r.WorkloadKind = parts[1], kind
		}
	}
	return r, r.Validate()
}
