package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/capability"
)

func TestRestrictedSetupHints(t *testing.T) {
	absent := func(source capability.Source) capability.SourceState {
		return capability.SourceState{Source: source, Availability: capability.Absent, Reason: capability.SourceAbsent}
	}
	forbidden := capability.SourceState{Source: capability.Cgroup, Availability: capability.Forbidden, Reason: capability.AccessDenied}

	for name, test := range map[string]struct {
		sources []capability.SourceState
		want    []string
	}{
		"nothing installed": {
			sources: []capability.SourceState{absent(capability.Cgroup), absent(capability.KubernetesMetrics)},
			want:    []string{"KubeMemLens is not installed", "Install metrics-server"},
		},
		"metrics-server only": {
			sources: []capability.SourceState{absent(capability.Cgroup), {Source: capability.KubernetesMetrics, Availability: capability.Available}},
			want:    []string{"KubeMemLens is not installed"},
		},
		"permission denied is not reported as missing": {
			sources: []capability.SourceState{forbidden},
		},
	} {
		var out bytes.Buffer
		if err := writeRestrictedSetupHints(&out, capability.Selection{Sources: test.sources}); err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		if out.Len() == 0 {
			lines = nil
		}
		if len(lines) != len(test.want) {
			t.Fatalf("%s: got %q", name, out.String())
		}
		for i, want := range test.want {
			if !strings.HasPrefix(lines[i], "Note: ") || !strings.Contains(lines[i], want) {
				t.Fatalf("%s: line %d = %q, want %q", name, i, lines[i], want)
			}
		}
	}
}
