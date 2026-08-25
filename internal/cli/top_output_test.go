package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

func TestPodsTableSeparatesPodAgeFromSampleAge(t *testing.T) {
	now := time.Now().UTC()
	var output bytes.Buffer
	printPodsTable(&output, []api.PodSnapshot{{
		Namespace: "default", PodName: "api", NodeName: "node-a",
		CapturedAt: now, Freshness: api.EvidenceFreshnessFresh,
		Context: api.PodContext{CreatedAt: now.Add(-48 * time.Hour)},
	}})
	text := output.String()
	for _, expected := range []string{"POD AGE", "SAMPLE", "STATE", "2d", "fresh"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("Pod table missing %q:\n%s", expected, text)
		}
	}
}
