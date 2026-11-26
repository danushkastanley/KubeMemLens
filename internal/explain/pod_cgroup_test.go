package explain

import (
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

// podLevelResourcesPod is the shape of a Pod that uses pod-level resources:
// container app has its own limit, container shared has none, and the Pod
// cgroup has a finite memory.max. Every container carries the same PodCgroup,
// as the collector requires.
func podLevelResourcesPod(t *testing.T, cgroup model.PodCgroupMemory, appMiB, sharedMiB uint64) api.PodSnapshot {
	t.Helper()
	app := limitedContainer("app", appMiB, 64)
	shared := limitedContainer("shared", sharedMiB, 0)
	shared.Memory.MaxUnlimited = true
	app.PodCgroup, shared.PodCgroup = cgroup, cgroup
	return podOf(t, app, shared)
}

func podCgroupLimit(currentMiB, maxMiB uint64) model.PodCgroupMemory {
	return model.PodCgroupMemory{CurrentKnown: true, CurrentBytes: currentMiB * mib, MaxKnown: true, MaxBytes: maxMiB * mib, LocalEventsKnown: true, LocalEventDeltasKnown: true}
}

func TestPodNearItsPodCgroupLimitIsLimitRisk(t *testing.T) {
	pod := podLevelResourcesPod(t, podCgroupLimit(240, 256), 30, 200)
	if !pod.Memory.MaxUnlimited {
		t.Fatalf("fixture needs an unlimited container sum, got %+v", pod.Memory)
	}

	result := AnalyzePod(pod)
	if result.Diagnosis != DiagnosisLimitRisk {
		t.Fatalf("diagnosis = %s, want limit-risk for a Pod at 94%% of its Pod cgroup memory.max; signals %v", result.Diagnosis, result.Signals)
	}
	signals := strings.Join(result.Signals, "\n")
	for _, want := range []string{"Pod cgroup usage is 94% of its 256Mi memory.max", "remaining Pod cgroup headroom is 16Mi"} {
		if !strings.Contains(signals, want) {
			t.Fatalf("signals missing %q:\n%s", want, signals)
		}
	}
	if strings.Contains(signals, "summed container limits") {
		t.Fatalf("signals describe the container sum when the Pod cgroup limit applies:\n%s", signals)
	}
}

// Without the Pod cgroup's own limit a Pod with an unlimited container is read
// as having no limit, so anonymous memory needs 512 MiB to be material. Under
// a 256 MiB Pod cgroup limit, half of that limit is the budget.
func TestPodCgroupLimitIsTheBudgetForAnonymousMemory(t *testing.T) {
	cgroup := podCgroupLimit(170, 256)
	pod := podLevelResourcesPod(t, cgroup, 10, 160)
	for index := range pod.Containers {
		pod.Containers[index].Memory.AnonBytes = pod.Containers[index].Memory.TotalBytes
	}
	pod.Memory.AnonBytes = pod.Memory.TotalBytes

	if got := AnalyzePod(pod).Diagnosis; got != DiagnosisRSSHeavy {
		t.Fatalf("diagnosis = %s, want rss-heavy: 170Mi of anonymous memory is over half of the 256Mi Pod cgroup limit", got)
	}
	pod.PodCgroup = model.PodCgroupMemory{}
	if got := AnalyzePod(pod).Diagnosis; got != DiagnosisNormal {
		t.Fatalf("diagnosis without the Pod cgroup = %s, want normal as before schema 8", got)
	}
}

func TestRecentPodCgroupEventsAreOOMEvidence(t *testing.T) {
	tests := map[string]struct {
		events model.PodCgroupMemory
		want   string
	}{
		"oom":      {model.PodCgroupMemory{LocalOOMEvents: 2, LocalOOMEventsDelta: 2}, "Pod cgroup recorded 2 OOM event(s) at its own limit in the sample window"},
		"oom kill": {model.PodCgroupMemory{LocalOOMKillEvents: 1, LocalOOMKillEventsDelta: 1}, "Pod cgroup recorded 1 OOM kill(s) at its own limit in the sample window"},
		// A container's recent memory.max events are OOM evidence; the Pod's are too.
		"max": {model.PodCgroupMemory{LocalMaxEvents: 5, LocalMaxEventsDelta: 5}, "Pod cgroup reached its own memory.max 5 time(s) in the sample window"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			cgroup := podCgroupLimit(100, 256)
			cgroup.LocalOOMEvents, cgroup.LocalOOMEventsDelta = test.events.LocalOOMEvents, test.events.LocalOOMEventsDelta
			cgroup.LocalOOMKillEvents, cgroup.LocalOOMKillEventsDelta = test.events.LocalOOMKillEvents, test.events.LocalOOMKillEventsDelta
			cgroup.LocalMaxEvents, cgroup.LocalMaxEventsDelta = test.events.LocalMaxEvents, test.events.LocalMaxEventsDelta
			result := AnalyzePod(podLevelResourcesPod(t, cgroup, 30, 60))
			if result.Diagnosis != DiagnosisOOMRisk || result.Confidence != ConfidenceHigh {
				t.Fatalf("diagnosis = %s confidence = %s, want oom-risk high", result.Diagnosis, result.Confidence)
			}
			if signals := strings.Join(result.Signals, "\n"); !strings.Contains(signals, test.want) {
				t.Fatalf("signals missing %q:\n%s", test.want, signals)
			}
		})
	}
}

// Counters without a delta may have been accumulated before the first sample.
func TestPodCgroupCountersWithoutDeltasAreNotRecent(t *testing.T) {
	cgroup := podCgroupLimit(100, 256)
	cgroup.LocalEventDeltasKnown = false
	cgroup.LocalOOMKillEvents, cgroup.LocalOOMEvents, cgroup.LocalMaxEvents = 3, 3, 9

	result := AnalyzePod(podLevelResourcesPod(t, cgroup, 30, 60))
	if result.Diagnosis == DiagnosisOOMRisk || strings.Contains(strings.Join(result.Signals, "\n"), "Pod cgroup recorded") {
		t.Fatalf("diagnosis = %s signals = %v; cumulative Pod cgroup counters were reported as recent", result.Diagnosis, result.Signals)
	}
}

func TestContainerOOMEvidenceAlsoNamesThePodCgroupLimit(t *testing.T) {
	pod := podLevelResourcesPod(t, podCgroupLimit(200, 256), 63, 100)
	pod.Memory.EventDeltasKnown, pod.Memory.OOMKillEvents, pod.Memory.OOMKillEventsDelta = true, 1, 1

	result := AnalyzePod(pod)
	signals := strings.Join(result.Signals, "\n")
	if result.Diagnosis != DiagnosisOOMRisk || !strings.Contains(signals, "Pod cgroup usage is 78% of its 256Mi memory.max") {
		t.Fatalf("diagnosis = %s, signals:\n%s", result.Diagnosis, signals)
	}
	if strings.Contains(signals, "of memory.max\n") || strings.HasSuffix(signals, "of memory.max") {
		t.Fatalf("signals describe the summed container usage against an unnamed memory.max:\n%s", signals)
	}
}

func TestClosestContainerIsComparedWithTheEffectiveLimit(t *testing.T) {
	quiet := podLevelResourcesPod(t, podCgroupLimit(128, 256), 60, 60)
	if got, ok := ClosestContainerToLimit(quiet); !ok || got != (ContainerLimitUsage{"app", 94}) {
		t.Fatalf("ClosestContainerToLimit = %+v, %v; want app at 94%% against a Pod at 50%%", got, ok)
	}
	full := podLevelResourcesPod(t, podCgroupLimit(250, 256), 32, 210)
	if got, ok := ClosestContainerToLimit(full); ok {
		t.Fatalf("ClosestContainerToLimit = %+v; app at 50%% is below the Pod at 98%%", got)
	}
}

func TestContainerLimitRiskStatesThePodCgroupFigure(t *testing.T) {
	pod := podLevelResourcesPod(t, podCgroupLimit(128, 256), 62, 60)
	result := AnalyzePod(pod)
	signals := strings.Join(result.Signals, "\n")
	if result.Diagnosis != DiagnosisLimitRisk || !strings.Contains(signals, "Pod cgroup usage is 50% of its 256Mi memory.max") {
		t.Fatalf("diagnosis = %s, signals:\n%s", result.Diagnosis, signals)
	}
}

func TestPodWithUnknownPodCgroupIsAnalysedAsBefore(t *testing.T) {
	pod := podOf(t, limitedContainer("app", 95, 100), limitedContainer("sidecar", 10, 100))
	if pod.PodCgroup != (model.PodCgroupMemory{}) {
		t.Fatalf("fixture carries a Pod cgroup: %+v", pod.PodCgroup)
	}
	got, want := AnalyzePodMemory(pod), Analyze(pod.Memory)
	if strings.Join(got.Signals, "|") != strings.Join(want.Signals, "|") || got.Diagnosis != want.Diagnosis || got.Confidence != want.Confidence {
		t.Fatalf("AnalyzePodMemory = %+v, want Analyze of the Pod memory %+v", got, want)
	}
}
