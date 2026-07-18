package recommend

import (
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/explain"
)

func TestRecommendationsAreCompositionAwareAndReadOnly(t *testing.T) {
	tests := map[explain.Diagnosis]string{
		explain.DiagnosisRSSHeavy:   "heap or allocator",
		explain.DiagnosisCacheHeavy: "file access",
		explain.DiagnosisTmpfsHeavy: "emptyDir",
		explain.DiagnosisSlabHeavy:  "socket churn",
	}
	for diagnosis, phrase := range tests {
		t.Run(string(diagnosis), func(t *testing.T) {
			items := ForFinding(explain.Result{Diagnosis: diagnosis})
			if len(items) < 2 || !strings.Contains(items[0].Action, phrase) {
				t.Fatalf("unexpected recommendations: %#v", items)
			}
			if items[len(items)-1].ID != "no-automatic-mutation" {
				t.Fatalf("missing safety recommendation: %#v", items)
			}
		})
	}
}
