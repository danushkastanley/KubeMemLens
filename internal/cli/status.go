package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/spf13/cobra"
)

type statusReport struct {
	Connection statusConnection `json:"connection"`
	Store      *api.DebugStore  `json:"store,omitempty"`
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
			report, err := buildStatusReport(cmd.Context(), collectorOptions())
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
		Data: statusData{Status: "failed"},
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
	report.Data.Status = "ok"
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
		text += fmt.Sprintf("\nStore:\n  containers: %d\n  stale containers: %d\n  pods: %d\n  namespaces: %d\n\nData:\n  status: %s\n\nHints:\n  kubectl logs -n kube-memlens ds/kube-memlens-agent\n  kubectl logs -n kube-memlens deploy/kube-memlens-collector\n",
			report.Store.TotalContainers,
			report.Store.StaleContainers,
			report.Store.Pods,
			report.Store.Namespaces,
			report.Data.Status,
		)
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
