package explain

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

func TestClosestContainerToLimitNamesWhatThePodFigureHides(t *testing.T) {
	unlimited := limitedContainer("sidecar", 900, 0)
	unlimited.Memory.MaxUnlimited = true
	unread := limitedContainer("metrics", 99, 100)
	unread.Memory.MaxKnown = false

	tests := []struct {
		name string
		pod  api.PodSnapshot
		want ContainerLimitUsage
		ok   bool
	}{
		{"container above the Pod figure", podOf(t, limitedContainer("app", 99, 100), limitedContainer("cache", 100, 1000)), ContainerLimitUsage{"app", 99}, true},
		{"single container repeats the Pod figure", podOf(t, limitedContainer("app", 99, 100)), ContainerLimitUsage{}, false},
		{"unlimited Pod reports the limited container", podOf(t, limitedContainer("app", 95, 100), unlimited), ContainerLimitUsage{"app", 95}, true},
		{"unread limit is never a candidate", podOf(t, limitedContainer("app", 10, 100), unread), ContainerLimitUsage{"app", 10}, true},
		{"no finite container limit", podOf(t, unlimited), ContainerLimitUsage{}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := ClosestContainerToLimit(test.pod)
			if got != test.want || ok != test.ok {
				t.Fatalf("ClosestContainerToLimit = %+v, %v; want %+v, %v (Pod memory %+v)", got, ok, test.want, test.ok, test.pod.Memory)
			}
		})
	}
}

// For any mix of finite, unlimited and unread container limits, a reported
// container has a finite limit, uses the largest share of it, and reads higher
// than the Pod figure whenever the Pod figure is itself a percentage.
func TestClosestContainerToLimitProperties(t *testing.T) {
	random := rand.New(rand.NewSource(316))
	for iteration := 0; iteration < 2000; iteration++ {
		containers := randomContainers(random)
		pod := podOf(t, containers...)
		got, ok := ClosestContainerToLimit(pod)

		best, bestPercent, candidates := -1.0, 0, 0
		for _, container := range containers {
			if !finiteLimit(container.Memory) {
				if ok && got.ContainerName == container.ContainerName {
					t.Fatalf("reported %s, whose limit is not finite: %+v", got.ContainerName, container.Memory)
				}
				continue
			}
			candidates++
			if ratio := container.Memory.LimitUsageRatio(); ratio > best {
				best, bestPercent = ratio, wholePercent(container.Memory)
			}
		}
		podPercent, podFinite := wholePercent(pod.Memory), finiteLimit(pod.Memory)
		wantOK := candidates > 0 && (!podFinite || bestPercent > podPercent)
		if ok != wantOK {
			t.Fatalf("reported=%v, want %v for containers %s (Pod finite=%v at %d%%)", ok, wantOK, describe(containers), podFinite, podPercent)
		}
		if ok && got.Percent != bestPercent {
			t.Fatalf("reported %+v, want the largest share %d%% for containers %s", got, bestPercent, describe(containers))
		}
	}
}

func TestClosestContainerToLimitSettlesTiesByName(t *testing.T) {
	for _, order := range [][]string{{"web", "api"}, {"api", "web"}} {
		pod := podOf(t, limitedContainer(order[0], 99, 100), limitedContainer(order[1], 99, 100), limitedContainer("idle", 1, 1000))
		if got, ok := ClosestContainerToLimit(pod); !ok || got.ContainerName != "api" {
			t.Fatalf("containers %v: got %+v, %v; want api", order, got, ok)
		}
	}
}

func randomContainers(random *rand.Rand) []api.ContainerSnapshot {
	count := 1 + random.Intn(4)
	containers := make([]api.ContainerSnapshot, 0, count)
	for index := 0; index < count; index++ {
		limit := uint64(1 + random.Intn(1024))
		container := limitedContainer(fmt.Sprintf("c%d", index), uint64(random.Intn(int(limit)+64)), limit)
		switch random.Intn(4) {
		case 0:
			container.Memory.MaxUnlimited, container.Memory.MaxBytes = true, 0
		case 1:
			container.Memory.MaxKnown, container.Memory.MaxBytes = false, 0
		}
		containers = append(containers, container)
	}
	return containers
}

func describe(containers []api.ContainerSnapshot) string {
	out := ""
	for _, container := range containers {
		memory := container.Memory
		out += fmt.Sprintf("[%s used=%d max=%d known=%v unlimited=%v] ", container.ContainerName, memory.TotalBytes, memory.MaxBytes, memory.MaxKnown, memory.MaxUnlimited)
	}
	return out
}
