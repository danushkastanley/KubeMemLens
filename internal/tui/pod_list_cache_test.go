package tui

import (
	"context"
	"fmt"
	"math/rand"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func generatedPods(random *rand.Rand, count int, capturedAt time.Time) []api.PodSnapshot {
	pods := make([]api.PodSnapshot, count)
	for index := range pods {
		memory := model.MemoryBreakdown{TotalBytes: uint64(random.Intn(40)), EventDeltasKnown: random.Intn(4) > 0}
		switch random.Intn(6) {
		case 0:
			memory.OOMKillEventsDelta = 1
		case 1:
			memory.OOMEventsDelta = 2
		case 2:
			memory.MaxEventsDelta = 1
		case 3:
			memory.PressureKnown, memory.PSIFullAvg10 = true, 0.2
		case 4:
			memory.MaxKnown, memory.MaxBytes = true, memory.TotalBytes+1
		}
		pod := api.PodSnapshot{
			Namespace: fmt.Sprintf("ns-%d", random.Intn(3)), PodName: fmt.Sprintf("pod-%03d", random.Intn(count)),
			NodeName: fmt.Sprintf("node-%d", random.Intn(2)), Memory: memory,
			CapturedAt: capturedAt.Add(-time.Duration(random.Intn(120)) * time.Second),
		}
		pod.Context.WorkloadKind, pod.Context.WorkloadName = "Deployment", fmt.Sprintf("app-%d", random.Intn(3))
		if random.Intn(5) == 0 {
			pod.Context.NodeMemoryPressure = "True"
		}
		pods[index] = pod
	}
	return pods
}

// The risk order used to come from a comparator that analysed both Pods on
// every comparison. This is that comparator, kept as the reference.
func sortPodsByRiskPerComparison(items []api.PodSnapshot, now time.Time, staleAfter time.Duration) {
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i], items[j]
		leftRisk, rightRisk := podRisk(left, now, staleAfter), podRisk(right, now, staleAfter)
		if leftRisk.score != rightRisk.score {
			return leftRisk.score > rightRisk.score
		}
		if left.Memory.TotalBytes != right.Memory.TotalBytes {
			return left.Memory.TotalBytes > right.Memory.TotalBytes
		}
		if left.Namespace != right.Namespace {
			return left.Namespace < right.Namespace
		}
		return left.PodName < right.PodName
	})
}

func podNames(items []api.PodSnapshot) []string {
	names := make([]string, len(items))
	for index, pod := range items {
		names[index] = fmt.Sprintf("%s/%s/%s/%d", pod.Namespace, pod.PodName, pod.NodeName, pod.Memory.TotalBytes)
	}
	return names
}

func TestRiskSortMatchesPerComparisonOrder(t *testing.T) {
	now := time.Now()
	for seed := int64(1); seed <= 200; seed++ {
		pods := generatedPods(rand.New(rand.NewSource(seed)), 60, now)
		expected := slices.Clone(pods)
		sortPodsByRiskPerComparison(expected, now, time.Minute)
		SortPodsAt(pods, sortRisk, now, time.Minute)
		if !slices.Equal(podNames(pods), podNames(expected)) {
			t.Fatalf("seed %d: risk order differs from the per-comparison order", seed)
		}
	}
}

func TestRiskSortScoresEachPodOnce(t *testing.T) {
	pods := generatedPods(rand.New(rand.NewSource(1)), 500, time.Now())
	scored := 0
	sortPodsByRisk(pods, func(pod api.PodSnapshot) int { scored++; return int(pod.Memory.TotalBytes) })
	if scored != len(pods) {
		t.Fatalf("scored %d times for %d Pods", scored, len(pods))
	}
}

// Every input the list depends on is changed at random. After each change the
// cached list must equal one built from scratch.
func TestCachedPodListMatchesUncachedAfterEveryChange(t *testing.T) {
	for seed := int64(1); seed <= 50; seed++ {
		random := rand.New(rand.NewSource(seed))
		m := newModel(context.Background(), Options{AllNamespaces: true}, nil, "test")
		m.lastRefresh = time.Now()
		m.data.Pods = generatedPods(random, 80, m.lastRefresh)
		for step := 0; step < 60; step++ {
			switch random.Intn(8) {
			case 0:
				m.data.Pods = generatedPods(random, 20+random.Intn(80), m.lastRefresh)
			case 1:
				m.query = []string{"", "pod-01", "state:stale", "state:fresh", "ns-1"}[random.Intn(5)]
			case 2:
				m.sort = []sortMode{sortRisk, sortTotal, sortRSS, sortName}[random.Intn(4)]
			case 3:
				m.currentNamespace = []string{"", "ns-0", "ns-2"}[random.Intn(3)]
			case 4:
				m.currentNode = []string{"", "node-1"}[random.Intn(2)]
			case 5:
				m.currentWorkloadKind, m.currentWorkloadName = "Deployment", []string{"", "app-1"}[random.Intn(2)]
			case 6:
				m.lastRefresh = m.lastRefresh.Add(30 * time.Second)
			}
			// A second call must not change the answer either.
			cached, again, fresh := m.visiblePods(), m.visiblePods(), m.buildVisiblePods()
			if !slices.Equal(podNames(cached), podNames(fresh)) || !slices.Equal(podNames(again), podNames(fresh)) {
				t.Fatalf("seed %d step %d: cached Pod list differs from a fresh one", seed, step)
			}
		}
	}
}

func BenchmarkMoveSelectionAndRenderFiveThousandPods(b *testing.B) {
	now := time.Now()
	pods := make([]api.PodSnapshot, 5000)
	for index := range pods {
		container := api.ContainerSnapshot{Memory: model.MemoryBreakdown{TotalBytes: 1 << 20, EventDeltasKnown: true}}
		pods[index] = api.PodSnapshot{
			Namespace: "load", PodName: fmt.Sprintf("pod-%05d", index), CapturedAt: now,
			Containers: []api.ContainerSnapshot{container, container},
			Memory:     model.MemoryBreakdown{TotalBytes: uint64(index + 1), EventDeltasKnown: true},
		}
	}
	m := newModel(context.Background(), Options{AllNamespaces: true}, nil, "test")
	m.width, m.height = 220, 50
	m.resizeViewports()
	m.loading = false
	m.data.Pods, m.lastRefresh = pods, now
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		next, _ := m.Update(keyMessage("j"))
		_ = next.View()
	}
}
