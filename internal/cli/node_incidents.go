package cli

import (
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/buildinfo"
	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/danushkastanley/kube-memlens/internal/nodeanalysis"
	"github.com/danushkastanley/kube-memlens/internal/nodeview"
	"github.com/spf13/cobra"
)

func captureNode(cmd *cobra.Command, options collectorOptionsProvider, name, output string, history, sensitive, force bool) error {
	reader, err := nodeCommandReader(cmd, options)
	if err != nil {
		return err
	}
	nodeReader, ok := reader.(incident.NodeCaptureReader)
	if !ok {
		return fmt.Errorf("Node capture requires the authenticated Kubernetes API connection and optional Node-context profile")
	}
	version := buildinfo.Current(runtime.Version(), runtime.GOOS, runtime.GOARCH).String()
	bundle, err := incident.CollectNode(cmd.Context(), nodeReader, name, incident.NodeCaptureOptions{Rank: nodeanalysis.Total, Limit: nodeanalysis.MaxContributors, IncludeHistory: history, IncludeSensitive: sensitive, ToolVersion: version})
	if err != nil {
		return err
	}
	if err := incident.WriteNode(cmd.OutOrStdout(), output, force, bundle); err != nil {
		return err
	}
	if output != "-" {
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s (Node incident schema 4; redacted=%t)\n", output, bundle.Redacted)
	}
	return err
}

func replayNode(w io.Writer, b incident.NodeBundle, name string) error {
	if name != "" && name != b.Evidence.Record.NodeName {
		return fmt.Errorf("selected Node is not in this incident")
	}
	lines := []string{fmt.Sprintf("Node incident captured: %s; redacted=%t", b.CapturedAt, b.Redacted)}
	lines = append(lines, nodeview.Lines(b.Evidence, b.CapturedAt, 100)...)
	if b.History != nil {
		lines = append(lines, "", "Captured history:")
		lines = append(lines, nodeview.HistoryLines(*b.History, 100)...)
	}
	_, err := fmt.Fprintln(w, strings.Join(lines, "\n"))
	return err
}

func compareNodeDocuments(w io.Writer, before, after incident.Document, name string) error {
	if before.Node == nil || after.Node == nil || name == "" {
		return fmt.Errorf("Node comparison requires two schema-4 captures and --node <name>")
	}
	if before.Node.Evidence.Record.NodeName != name || after.Node.Evidence.Record.NodeName != name {
		return fmt.Errorf("selected Node is not present in both captures")
	}
	if err := incident.CompatibleNodes(*before.Node, *after.Node); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w, strings.Join(nodeview.ComparisonLines(before.Node.Evidence, after.Node.Evidence, 100), "\n"))
	return err
}
