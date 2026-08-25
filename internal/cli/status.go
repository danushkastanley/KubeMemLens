package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/spf13/cobra"
)

type statusReport struct {
	Connection statusConnection `json:"connection"`
	Store      *api.DebugStore  `json:"store,omitempty"`
	Metrics    *statusMetrics   `json:"metrics,omitempty"`
	Data       statusData       `json:"data"`
	Error      string           `json:"error,omitempty"`
}

type statusConnection struct {
	Mode        string `json:"mode"`
	Collector   string `json:"collector"`
	Healthy     bool   `json:"healthy"`
	Description string `json:"description"`
}

type statusData struct {
	Status string `json:"status"`
}

type statusMetrics struct {
	Endpoint string `json:"endpoint"`
	Enabled  string `json:"enabled"`
	Hint     string `json:"hint"`
}

func newStatusCommand(collectorOptions collectorOptionsProvider) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check collector connectivity and latest snapshot counts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if output != "text" && output != "json" {
				return fmt.Errorf("invalid output %q, want text or json", output)
			}
			opts, err := withReadScope(collectorOptions(), "", true)
			if err != nil {
				return err
			}
			report, err := buildStatusReport(cmd.Context(), opts)
			if output == "json" {
				encoded, marshalErr := json.MarshalIndent(report, "", "  ")
				if marshalErr != nil {
					return marshalErr
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
				if err != nil {
					return errors.New("status check failed")
				}
				return nil
			}
			if err != nil {
				return errors.New(renderStatusReport(report))
			}
			fmt.Fprint(cmd.OutOrStdout(), renderStatusReport(report))
			return nil
		},
	}
	cmd.Flags().StringVar(&output, "output", "text", "output format: text or json")
	return cmd
}

func buildStatusReport(ctx context.Context, opts client.Options) (statusReport, error) {
	mode, modeErr := client.ResolveMode(opts)
	description := client.Describe(opts)
	report := statusReport{
		Connection: statusConnection{
			Mode:        string(mode),
			Collector:   description,
			Description: description,
		},
		Data: statusData{Status: string(api.CollectorUnavailable)},
	}
	if modeErr != nil {
		report.Error = modeErr.Error()
		return report, modeErr
	}

	reader, description, err := client.NewSnapshotReader(ctx, opts)
	report.Connection.Collector = description
	report.Connection.Description = description
	if err != nil {
		report.Error = client.ConnectionError(opts, description, err).Error()
		return report, err
	}
	if err := reader.Health(ctx); err != nil {
		report.Error = client.ConnectionError(opts, description, err).Error()
		return report, err
	}
	report.Connection.Healthy = true

	store, err := reader.DebugStore(ctx)
	if err != nil {
		report.Error = client.ConnectionError(opts, description, err).Error()
		return report, err
	}
	report.Store = &store
	report.Metrics = &statusMetrics{
		Endpoint: "/metrics",
		Enabled:  "unknown",
		Hint:     "scrape collector service port 8080 path /metrics",
	}
	if mode == client.ConnectionModeKubernetesAPI {
		report.Metrics.Endpoint = "/apis/memory.kubememlens.io/v1alpha1/metrics/current"
		report.Metrics.Hint = "read the separately authorised metrics resource through the Kubernetes API"
	}
	report.Data.Status = string(store.Reliability.State)
	return report, nil
}

func renderStatusReport(report statusReport) string {
	health := "failed"
	if report.Connection.Healthy {
		health = "ok"
	}

	text := fmt.Sprintf("KubeMemLens status\n\nConnection:\n  mode: %s\n  collector: %s\n  health: %s\n",
		report.Connection.Mode,
		report.Connection.Collector,
		health,
	)
	if report.Store != nil {
		text += fmt.Sprintf("\nStore:\n  containers: %d\n  stale containers: %d\n  pods: %d\n  namespaces: %d\n",
			report.Store.TotalContainers,
			report.Store.StaleContainers,
			report.Store.Pods,
			report.Store.Namespaces,
		)
		reliability := report.Store.Reliability
		text += fmt.Sprintf("\nReliability:\n  state: %s\n  completeness: %s\n  generation: %s\n  expected nodes: %d\n  fresh nodes: %d\n  stale nodes: %d\n  missing nodes: %d\n  inventory updated: %s\n  history reset: %s\n  history completeness: %s\n",
			reliability.State,
			reliability.Completeness,
			reliability.Generation,
			reliability.ExpectedNodes,
			reliability.FreshNodes,
			reliability.StaleNodes,
			reliability.MissingNodes,
			reliability.InventoryUpdatedAt.Format(time.RFC3339),
			reliability.History.ResetAt.Format(time.RFC3339),
			reliability.History.Completeness,
		)
		if report.Metrics != nil {
			text += fmt.Sprintf("\nMetrics:\n  endpoint: %s\n  enabled: %s\n  hint: %s\n",
				report.Metrics.Endpoint,
				report.Metrics.Enabled,
				report.Metrics.Hint,
			)
		}
		text += fmt.Sprintf("\nData:\n  status: %s\n\nHints:\n  kubectl logs -n kube-memlens ds/kube-memlens-agent\n  kubectl logs -n kube-memlens deploy/kube-memlens-collector\n", report.Data.Status)
		return text
	}

	text += "\nError:\n" + indentBlock(report.Error) + "\n"
	return text
}

func indentBlock(value string) string {
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		lines[i] = "  " + line
	}
	return strings.Join(lines, "\n")
}
