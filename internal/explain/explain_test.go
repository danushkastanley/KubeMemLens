package explain

import (
	"path/filepath"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/cgroup"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestAnalyzeSamples(t *testing.T) {
	tests := map[string]Diagnosis{
		"cache-heavy": DiagnosisCacheHeavy,
		"rss-heavy":   DiagnosisRSSHeavy,
		"tmpfs-heavy": DiagnosisTmpfsHeavy,
		"dirty-heavy": DiagnosisDirtyWritebackHeavy,
		"normal":      DiagnosisNormal,
	}

	root := filepath.Join("..", "..", "examples", "cgroup-v2")
	for sample, want := range tests {
		t.Run(sample, func(t *testing.T) {
			breakdown, err := cgroup.ParseDirectory(sample, filepath.Join(root, sample))
			if err != nil {
				t.Fatalf("ParseDirectory returned error: %v", err)
			}

			if got := Analyze(breakdown).Diagnosis; got != want {
				t.Fatalf("Analyze(%s) = %s, want %s", sample, got, want)
			}
		})
	}
}

func TestAnalyzeOOMRisk(t *testing.T) {
	breakdown := model.MemoryBreakdown{
		Name:       "oom",
		TotalBytes: 1000,
		AnonBytes:  200,
		OOMEvents:  1,
	}

	result := Analyze(breakdown)
	if result.Diagnosis != DiagnosisOOMRisk {
		t.Fatalf("Diagnosis = %s, want %s", result.Diagnosis, DiagnosisOOMRisk)
	}
	if len(result.Signals) == 0 {
		t.Fatal("expected OOM signals")
	}
}
