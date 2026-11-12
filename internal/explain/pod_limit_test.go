package explain

import (
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/aggregate"
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func limitedContainer(name string, usedMiB, limitMiB uint64) api.ContainerSnapshot {
	return api.ContainerSnapshot{
		Namespace: "shop", PodName: "checkout", PodUID: "uid-1", NodeName: "node-a", ContainerName: name, ContainerID: name,
		Memory: model.MemoryBreakdown{Name: name, TotalBytes: usedMiB * mib, AnonBytes: usedMiB * mib / 4, MaxKnown: true, MaxBytes: limitMiB * mib},
	}
}

func podOf(t *testing.T, containers ...api.ContainerSnapshot) api.PodSnapshot {
	t.Helper()
	pods := aggregate.Pods(containers)
	if len(pods) != 1 {
		t.Fatalf("fixture built %d Pods, want 1", len(pods))
	}
	return pods[0]
}

// The kernel kills at each container's memory.max, so a Pod whose summed usage
// is far from its summed limits can still hold a container about to be killed.
func TestPodDiagnosisPromotesTheContainerNearestItsOwnLimit(t *testing.T) {
	pod := podOf(t, limitedContainer("app", 99, 100), limitedContainer("sidecar", 100, 1024), limitedContainer("metrics", 95, 100))
	if ratio := pod.Memory.LimitUsageRatio(); ratio > 0.5 {
		t.Fatalf("fixture Pod is at %.0f%% of its summed limits; the test needs a low Pod figure", ratio*100)
	}

	result := AnalyzePod(pod)
	if result.Diagnosis != DiagnosisLimitRisk || result.Severity != SeverityMedium {
		t.Fatalf("diagnosis = %s severity = %s, want limit-risk medium", result.Diagnosis, result.Severity)
	}
	if !strings.Contains(result.Summary, "app") {
		t.Fatalf("summary does not name the container: %q", result.Summary)
	}
	signals := strings.Join(result.Signals, "\n")
	for _, want := range []string{"container app is at 99% of its memory.max", "remaining container headroom is 1Mi", "Pod usage is 24% of the summed container limits"} {
		if !strings.Contains(signals, want) {
			t.Fatalf("signals missing %q:\n%s", want, signals)
		}
	}
	if strings.Contains(signals, "metrics") {
		t.Fatalf("signals name a container that is not the nearest to its limit:\n%s", signals)
	}
}

func TestPodDiagnosisKeepsStrongerPodEvidenceOverContainerLimit(t *testing.T) {
	killed := limitedContainer("sidecar", 100, 1024)
	killed.Memory.EventDeltasKnown, killed.Memory.OOMKillEventsDelta = true, 1
	if got := AnalyzePod(podOf(t, limitedContainer("app", 99, 100), killed)).Diagnosis; got != DiagnosisOOMRisk {
		t.Fatalf("diagnosis = %s, want oom-risk to outrank a container limit", got)
	}
}

func TestPodDiagnosisWithoutContainerLimitRiskIsUnchanged(t *testing.T) {
	tests := map[string]api.PodSnapshot{
		"containers have headroom": podOf(t, limitedContainer("app", 50, 100), limitedContainer("sidecar", 100, 1024)),
		"summary without containers": {
			Namespace: "shop", PodName: "checkout",
			Memory: model.MemoryBreakdown{TotalBytes: 199 * mib, MaxKnown: true, MaxBytes: 1124 * mib},
		},
	}
	for name, pod := range tests {
		t.Run(name, func(t *testing.T) {
			if got := AnalyzePod(pod).Diagnosis; got == DiagnosisLimitRisk {
				t.Fatalf("diagnosis = %s without a container near its limit", got)
			}
		})
	}
}

// One container with a limit and one whose limit was not read must not be
// reported against the known limit alone.
func TestPodWithAnUnreadContainerLimitIsNotLimitRisk(t *testing.T) {
	unread := limitedContainer("sidecar", 90, 0)
	unread.Memory.MaxKnown = false
	pod := podOf(t, limitedContainer("app", 60, 100), unread)

	if pod.Memory.MaxKnown {
		t.Fatalf("Pod limit reported as known with an unread container limit: %+v", pod.Memory)
	}
	result := AnalyzePod(pod)
	if result.Diagnosis == DiagnosisLimitRisk || strings.Contains(strings.Join(result.Signals, "\n"), "150%") {
		t.Fatalf("diagnosis = %s signals = %v", result.Diagnosis, result.Signals)
	}
}
