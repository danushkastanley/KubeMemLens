package cli

import (
	"fmt"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/capability"
)

func renderEvidencePlan(plan capability.Selection) string {
	var out strings.Builder
	fmt.Fprintf(&out, "\nEvidence:\n  mode: %s\n  state: %s\n  freshness: %s\n  completeness: %s\n",
		plan.Label(), plan.State, plan.Freshness, plan.Completeness)
	for _, source := range plan.Sources {
		fmt.Fprintf(&out, "  %s: %s", source.Source, source.Availability)
		if source.APIVersion != "" {
			fmt.Fprintf(&out, " [%s]", source.APIVersion)
		}
		if source.Reason != "" {
			fmt.Fprintf(&out, " (%s)", source.Reason)
		}
		out.WriteByte('\n')
	}
	for _, query := range plan.Queries {
		if query.Availability != capability.Available {
			fmt.Fprintf(&out, "  %s unavailable: %s\n", query.Query, query.Reason)
		}
	}
	return out.String()
}
