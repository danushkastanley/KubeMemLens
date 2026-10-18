package cli

import (
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/buildinfo"
	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/danushkastanley/kube-memlens/internal/nodeanalysis"
	"github.com/danushkastanley/kube-memlens/internal/nodeview"
	"github.com/spf13/cobra"
)

func captureTopology(cmd *cobra.Command, options collectorOptionsProvider, name, output string, history, sensitive, force bool) error {
	reader, err := nodeCommandReader(cmd, options)
	if err != nil {
		return err
	}
	topologyReader, ok := reader.(incident.TopologyCaptureReader)
	if !ok {
		return fmt.Errorf("topology capture requires the authenticated Kubernetes API connection")
	}
	version := buildinfo.Current(runtime.Version(), runtime.GOOS, runtime.GOARCH).String()
	b, err := incident.CollectTopology(cmd.Context(), topologyReader, name, incident.NodeCaptureOptions{Rank: nodeanalysis.Total, Limit: nodeanalysis.MaxContributors, IncludeHistory: history, IncludeSensitive: sensitive, ToolVersion: version})
	if err != nil {
		return err
	}
	if err := incident.WriteTopology(cmd.OutOrStdout(), output, force, b); err != nil {
		return err
	}
	if output != "-" {
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s (Node topology incident schema 7; redacted=%t)\n", output, b.Redacted)
	}
	return err
}
func replayTopology(w io.Writer, b incident.TopologyBundle, name string) error {
	if err := replayNode(w, b.Node, name); err != nil {
		return err
	}
	lines := nodeview.TopologyLines(api.NodeMemoryTopology{Current: b.Current, LastGood: b.LastGood}, b.CapturedAt, 100)
	lines = append(lines, b.Caveats...)
	_, err := fmt.Fprintln(w, strings.Join(lines, "\n"))
	return err
}
