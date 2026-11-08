package cli

import (
	"fmt"
	"io"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/client"
)

// restrictedSetupHints explains, in plain terms, why restricted output is thin.
// It uses only discovery results, never cluster identities.
func restrictedSetupHints(plan capability.Selection) []string {
	var hints []string
	for _, source := range plan.Sources {
		if source.Availability != capability.Absent || source.Reason != capability.SourceAbsent {
			continue
		}
		switch source.Source {
		case capability.Cgroup:
			hints = append(hints, "KubeMemLens is not installed in this cluster, so cgroup memory detail is unavailable. Install guide: "+client.InstallGuideURL)
		case capability.KubernetesMetrics:
			hints = append(hints, "The Kubernetes Metrics API is not available, so working set is unreported. Install metrics-server to see working set without the KubeMemLens chart.")
		}
	}
	return hints
}

// writeRestrictedSetupHints is for human-readable output only. Callers skip it
// for json, yaml and csv so scripted use sees no extra text.
func writeRestrictedSetupHints(w io.Writer, plan capability.Selection) error {
	for _, hint := range restrictedSetupHints(plan) {
		if _, err := fmt.Fprintf(w, "Note: %s\n", hint); err != nil {
			return err
		}
	}
	return nil
}
