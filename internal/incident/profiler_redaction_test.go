package incident

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/profiler"
)

func TestPublicCaptureOmitsProfilerDeclarationsAndEndpointCredentials(t *testing.T) {
	labels := map[string]string{profiler.LabelPrefix + "app": profiler.GoHeapProfile, "profile-endpoint": "https://user:secret@example.test/heap"}
	bundle := api.IncidentBundle{Pods: []api.PodSnapshot{{Context: api.PodContext{Labels: labels}, Containers: []api.ContainerSnapshot{{Context: api.ContainerContext{Labels: labels}}}}}}
	Redact(&bundle)
	body, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"profiling.", "go-heap-pprof", "secret", "example.test"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("capture leaks %s", forbidden)
		}
	}
	if labels[profiler.LabelPrefix+"app"] != profiler.GoHeapProfile {
		t.Fatal("redaction mutated live metadata map")
	}
}
