package explain

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
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
	if result.Confidence != ConfidenceMedium {
		t.Fatalf("Confidence = %s, want medium for cumulative event", result.Confidence)
	}
}

func TestAnalyzePressureRisk(t *testing.T) {
	result := Analyze(model.MemoryBreakdown{
		TotalBytes:            512 * mib,
		PressureKnown:         true,
		PSISomeAvg10:          2.5,
		PSIFullAvg10:          0.2,
		LocalEventsKnown:      true,
		LocalEventDeltasKnown: true,
		LocalHighEventsDelta:  3,
	})
	if result.Diagnosis != DiagnosisPressure {
		t.Fatalf("Diagnosis = %s, want %s", result.Diagnosis, DiagnosisPressure)
	}
	if result.Confidence != ConfidenceHigh {
		t.Fatalf("Confidence = %s, want high", result.Confidence)
	}
}

func TestAnalyzeLimitRisk(t *testing.T) {
	result := Analyze(model.MemoryBreakdown{
		TotalBytes: 950 * mib,
		MaxKnown:   true,
		MaxBytes:   1000 * mib,
	})
	if result.Diagnosis != DiagnosisLimitRisk {
		t.Fatalf("Diagnosis = %s, want %s", result.Diagnosis, DiagnosisLimitRisk)
	}
	if result.Confidence != ConfidenceMedium {
		t.Fatalf("Confidence = %s, want medium", result.Confidence)
	}
}

func TestAnalyzeKernelFacingSecondaryDetail(t *testing.T) {
	result := Analyze(model.MemoryBreakdown{TotalBytes: 100, SocketBytes: 12, PageTableBytes: 11, SlabUnreclaimableBytes: 10})
	if result.Diagnosis != DiagnosisSlabHeavy {
		t.Fatalf("Diagnosis = %s, want %s", result.Diagnosis, DiagnosisSlabHeavy)
	}
	joined := strings.Join(result.Signals, "\n")
	for _, want := range []string{"unreclaimable slab", "socket memory", "page tables"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("signals missing %q: %#v", want, result.Signals)
		}
	}
}

func TestAnalyzePodUsesRecentOOMTerminationAfterCgroupReset(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	result := AnalyzePodAt(api.PodSnapshot{
		Context: api.PodContext{
			LastTerminationKnown:      true,
			LastTerminationReason:     "OOMKilled",
			LastTerminationExitCode:   137,
			LastTerminationFinishedAt: now.Add(-2 * time.Minute),
		},
		Memory: model.MemoryBreakdown{TotalBytes: 100 * mib, AnonBytes: 20 * mib},
	}, now)
	if result.Diagnosis != DiagnosisOOMRisk {
		t.Fatalf("Diagnosis = %s, want %s", result.Diagnosis, DiagnosisOOMRisk)
	}
	if result.Confidence != ConfidenceHigh {
		t.Fatalf("Confidence = %s, want high", result.Confidence)
	}
}

func TestAnalyzePodDoesNotTreatOldOOMTerminationAsRecent(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	result := AnalyzePodAt(api.PodSnapshot{
		Context: api.PodContext{
			LastTerminationKnown:      true,
			LastTerminationReason:     "OOMKilled",
			LastTerminationFinishedAt: now.Add(-time.Hour),
		},
		Memory: model.MemoryBreakdown{TotalBytes: 100 * mib, AnonBytes: 20 * mib},
	}, now)
	if result.Diagnosis == DiagnosisOOMRisk {
		t.Fatalf("old termination produced OOM risk: %#v", result)
	}
}

func TestAnalyzePodIncludesNodeMemoryPressure(t *testing.T) {
	result := AnalyzePodAt(api.PodSnapshot{
		Context: api.PodContext{NodeMemoryPressure: "True"},
		Memory:  model.MemoryBreakdown{TotalBytes: 100 * mib, AnonBytes: 20 * mib},
	}, time.Unix(1_700_000_000, 0).UTC())
	if result.Diagnosis != DiagnosisPressure {
		t.Fatalf("Diagnosis = %s, want %s", result.Diagnosis, DiagnosisPressure)
	}
}

func TestAnalyzePodIncludesRuntimeEmptyDirAndAllocatableContext(t *testing.T) {
	pod := api.PodSnapshot{
		Containers: []api.ContainerSnapshot{{ContainerName: "app"}},
		Context: api.PodContext{
			RuntimeClassName:           "gvisor",
			MemoryEmptyDirCount:        2,
			MemoryEmptyDirLimited:      1,
			MemoryEmptyDirLimitBytes:   64 << 20,
			NodeMemoryPressure:         "True",
			NodeMemoryAllocatableKnown: true,
			NodeMemoryAllocatable:      8 << 30,
		},
	}
	result := AnalyzePod(pod)
	joined := strings.Join(append(result.Signals, result.SuggestedChecks...), "\n")
	for _, want := range []string{"runtimeClass=gvisor", "memory-backed emptyDir volumes=2", "unbounded=1", "node allocatable memory=8.00Gi"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("context output missing %q: %#v", want, result)
		}
	}
}

func TestAnalyzeDoesNotRepeatHistoricalOOMDiagnosis(t *testing.T) {
	breakdown := model.MemoryBreakdown{
		TotalBytes:         1000,
		AnonBytes:          200,
		OOMEvents:          5,
		EventDeltasKnown:   true,
		OOMEventsDelta:     0,
		OOMKillEventsDelta: 0,
		MaxEventsDelta:     0,
	}

	if got := Analyze(breakdown).Diagnosis; got == DiagnosisOOMRisk {
		t.Fatalf("Diagnosis = %s, historical cumulative event should not repeat OOM risk", got)
	}
}

func TestEveryDiagnosisIncludesSeverityConfidenceEvidenceChecksAndCaveats(t *testing.T) {
	tests := map[Diagnosis]model.MemoryBreakdown{
		DiagnosisOOMRisk:             {TotalBytes: 100, OOMEvents: 1},
		DiagnosisPressure:            {TotalBytes: 100, PressureKnown: true, PSISomeAvg10: 1},
		DiagnosisLimitRisk:           {TotalBytes: 95, MaxKnown: true, MaxBytes: 100},
		DiagnosisCacheHeavy:          {TotalBytes: 100, FileBytes: 80, InactiveFileBytes: 60},
		DiagnosisRSSHeavy:            {TotalBytes: 100, AnonBytes: 80},
		DiagnosisTmpfsHeavy:          {TotalBytes: 100, ShmemBytes: 40},
		DiagnosisDirtyWritebackHeavy: {TotalBytes: 100, DirtyBytes: 20},
		DiagnosisSlabHeavy:           {TotalBytes: 100, SlabBytes: 30},
		DiagnosisMixed:               {TotalBytes: 100, AnonBytes: 20, FileBytes: 50, SlabBytes: 30},
		DiagnosisNormal:              {TotalBytes: 100, AnonBytes: 20},
	}
	for want, memory := range tests {
		result := Analyze(memory)
		if result.Diagnosis != want {
			t.Fatalf("Diagnosis = %s, want %s for %#v", result.Diagnosis, want, memory)
		}
		if result.Severity == "" || result.Confidence == "" || result.ConfidenceReason == "" {
			t.Fatalf("finding metadata missing for %s: %#v", result.Diagnosis, result)
		}
		if len(result.Signals) == 0 || len(result.SuggestedChecks) == 0 || len(result.Caveats) == 0 {
			t.Fatalf("finding contract incomplete for %s: %#v", result.Diagnosis, result)
		}
	}
}

func TestEvidenceWindowReportsExactUniformAndMixedBounds(t *testing.T) {
	base := time.Date(2026, 7, 18, 12, 0, 0, 123, time.UTC)
	uniform := AnalyzePodAt(api.PodSnapshot{
		CapturedAt: base.Add(10 * time.Second),
		Containers: []api.ContainerSnapshot{
			{CapturedAt: base.Add(10 * time.Second), DeltaStartedAt: base, DeltaWindowKnown: true},
			{CapturedAt: base.Add(10 * time.Second), DeltaStartedAt: base, DeltaWindowKnown: true},
		},
	}, base.Add(10*time.Second))
	if !uniform.EvidenceWindow.DeltaKnown || !uniform.EvidenceWindow.DeltaComplete || !uniform.EvidenceWindow.DeltaUniform {
		t.Fatalf("uniform window flags = %#v", uniform.EvidenceWindow)
	}
	if !uniform.EvidenceWindow.DeltaStart.Equal(base) || !uniform.EvidenceWindow.DeltaEnd.Equal(base.Add(10*time.Second)) {
		t.Fatalf("uniform window bounds = %#v", uniform.EvidenceWindow)
	}

	mixed := AnalyzeWorkload(api.WorkloadSnapshot{Pods: []api.PodSnapshot{
		{CapturedAt: base.Add(10 * time.Second), Containers: []api.ContainerSnapshot{{CapturedAt: base.Add(10 * time.Second), DeltaStartedAt: base, DeltaWindowKnown: true}}},
		{CapturedAt: base.Add(12 * time.Second), Containers: []api.ContainerSnapshot{{CapturedAt: base.Add(12 * time.Second), DeltaStartedAt: base.Add(time.Second), DeltaWindowKnown: true}}},
	}})
	if !mixed.EvidenceWindow.DeltaComplete || mixed.EvidenceWindow.DeltaUniform || mixed.EvidenceWindow.DeltaWindowCount != 2 {
		t.Fatalf("mixed window flags = %#v", mixed.EvidenceWindow)
	}
	if !strings.Contains(strings.Join(mixed.Caveats, "\n"), "multiple exact counter windows") {
		t.Fatalf("mixed-window caveat missing: %#v", mixed.Caveats)
	}
}
