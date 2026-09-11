package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/nodeanalysis"
	"github.com/danushkastanley/kube-memlens/internal/nodeview"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

func newExplainNodeCommand(options collectorOptionsProvider) *cobra.Command {
	var output, rank string
	var limit int
	cmd := &cobra.Command{Use: "node <node-name>", Short: "Explain Node memory and authorised contributors", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateNodeOutput(output); err != nil {
			return err
		}
		if err := validateNodeRank(rank, limit); err != nil {
			return err
		}
		reader, err := nodeCommandReader(cmd, options)
		if err != nil {
			return err
		}
		evidenceReader, ok := reader.(client.NodeEvidenceReader)
		if !ok {
			return fmt.Errorf("Node analysis requires the authenticated Kubernetes API connection and optional Node-context profile")
		}
		evidence, err := client.ReadNodeEvidence(cmd.Context(), evidenceReader, args[0], nodeanalysis.Metric(rank), limit)
		if err != nil {
			return err
		}
		if output != "text" {
			return writeNodeDocument(cmd.OutOrStdout(), output, evidence)
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), strings.Join(nodeview.Lines(evidence, time.Now().UTC(), 100), "\n"))
		return err
	}}
	cmd.Flags().StringVarP(&output, "output", "o", "text", "output format: text, json, or yaml")
	cmd.Flags().StringVar(&rank, "rank", "total", "rank contributors by total, anon, cache, shmem, residual, psi, or oom")
	cmd.Flags().IntVar(&limit, "limit", nodeanalysis.DefaultContributors, "maximum contributors per ranking group (1-100)")
	return cmd
}

func newHistoryNodeCommand(options collectorOptionsProvider) *cobra.Command {
	var output string
	var since time.Duration
	cmd := &cobra.Command{Use: "node <node-name>", Short: "Show bounded Node history with source times and coverage", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateNodeOutput(output); err != nil {
			return err
		}
		if since < 0 || since > 15*time.Minute {
			return fmt.Errorf("--since must be between 0 and 15m")
		}
		reader, err := nodeCommandReader(cmd, options)
		if err != nil {
			return err
		}
		historyReader, ok := reader.(client.NodeHistoryReader)
		if !ok {
			return fmt.Errorf("Node history requires the authenticated Kubernetes API connection and optional Node-context profile")
		}
		history, err := client.ReadNodeHistory(cmd.Context(), historyReader, args[0])
		if err != nil {
			return err
		}
		if since > 0 {
			history = nodeview.HistorySince(history, time.Now().UTC().Add(-since))
		}
		if output != "text" {
			return writeNodeDocument(cmd.OutOrStdout(), output, history)
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), strings.Join(nodeview.HistoryLines(history, 100), "\n"))
		return err
	}}
	cmd.Flags().StringVarP(&output, "output", "o", "text", "output format: text, json, or yaml")
	cmd.Flags().DurationVar(&since, "since", 0, "show source samples from the last duration, up to 15m")
	return cmd
}

func nodeCommandReader(cmd *cobra.Command, options collectorOptionsProvider) (client.SnapshotReader, error) {
	opts, err := withReadScope(options(), "", true)
	if err != nil {
		return nil, err
	}
	reader, description, err := client.NewSnapshotReader(cmd.Context(), opts)
	if err != nil {
		return nil, collectorUnavailableError(opts, description, err)
	}
	return reader, nil
}

func validateNodeRank(rank string, limit int) error {
	switch nodeanalysis.Metric(rank) {
	case nodeanalysis.Total, nodeanalysis.Anon, nodeanalysis.Cache, nodeanalysis.Shmem, nodeanalysis.Residual, nodeanalysis.PSI, nodeanalysis.OOM:
	default:
		return fmt.Errorf("--rank must be total, anon, cache, shmem, residual, psi, or oom")
	}
	if limit < 1 || limit > nodeanalysis.MaxContributors {
		return fmt.Errorf("--limit must be between 1 and 100")
	}
	return nil
}

func validateNodeOutput(output string) error {
	if output != "text" && output != "json" && output != "yaml" {
		return fmt.Errorf("output must be text, json, or yaml")
	}
	return nil
}

func writeNodeDocument(w io.Writer, output string, value any) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if output == "yaml" {
		body, err = yaml.JSONToYAML(body)
		if err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(w, string(body))
	return err
}
