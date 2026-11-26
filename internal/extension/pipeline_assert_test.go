package extension_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

// memoryTotals are the figures a Pod, workload or namespace row reports, taken
// from the fixture files rather than from anything the pipeline returned.
type memoryTotals struct {
	total, anon, file, shmem uint64
	pods, containers         int
}

func (m *memoryTotals) add(container fixtureContainer) {
	m.total += container.current
	m.anon += container.anon
	m.file += container.file
	m.shmem += container.shmem
	m.containers++
}

func totalsOf(memory model.MemoryBreakdown) memoryTotals {
	return memoryTotals{total: memory.TotalBytes, anon: memory.AnonBytes, file: memory.FileBytes, shmem: memory.ShmemBytes}
}

func assertClientReadsFixture(t *testing.T, ctx context.Context, reader *client.KubernetesAPIClient, fixture *pipelineFixture) {
	t.Helper()
	assertContainersMatchFixture(t, ctx, reader, fixture)
	assertAggregatesMatchFixture(t, ctx, reader, fixture)
}

// assertContainersMatchFixture checks each container the client reads against
// the files in its cgroup, and that nothing else is attributed to a Pod.
func assertContainersMatchFixture(t *testing.T, ctx context.Context, reader *client.KubernetesAPIClient, fixture *pipelineFixture) {
	t.Helper()
	containers, err := reader.Containers(ctx)
	if err != nil {
		t.Fatalf("client Containers: %v", err)
	}
	read := map[string]api.ContainerSnapshot{}
	for _, container := range containers {
		if container.ContainerID == fixture.unmappedID && container.Namespace != "" {
			t.Errorf("unmapped cgroup %s was attributed to %s/%s", container.ContainerID, container.Namespace, container.PodName)
		}
		if container.Namespace != "" {
			read[container.ContainerID] = container
		}
	}
	expected := 0
	for _, pod := range fixture.pods {
		for _, want := range pod.containers {
			expected++
			got, found := read[want.id]
			if !found {
				t.Errorf("container %s/%s/%s is in the cgroup tree but the client did not read it", pod.namespace, pod.name, want.name)
				continue
			}
			if got.Namespace != pod.namespace || got.PodName != pod.name || got.ContainerName != want.name {
				t.Errorf("container %s read as %s/%s/%s, want %s/%s/%s", want.id[:12], got.Namespace, got.PodName, got.ContainerName, pod.namespace, pod.name, want.name)
			}
			assertContainerMemory(t, pod.name+"/"+want.name, want, got.Memory)
		}
	}
	if len(read) != expected {
		t.Errorf("client read %d Pod containers, the tree holds %d: a removed container is still reported or one is duplicated", len(read), expected)
	}
}

func assertContainerMemory(t *testing.T, name string, want fixtureContainer, got model.MemoryBreakdown) {
	t.Helper()
	wantTotals := memoryTotals{total: want.current, anon: want.anon, file: want.file, shmem: want.shmem}
	if totalsOf(got) != wantTotals {
		t.Errorf("%s memory = %+v, the cgroup files hold %+v", name, totalsOf(got), wantTotals)
	}
	switch want.limit {
	case "":
		if got.MaxKnown || got.MaxUnlimited || got.MaxBytes != 0 {
			t.Errorf("%s has no memory.max but its limit read as known=%t unlimited=%t bytes=%d, want unknown", name, got.MaxKnown, got.MaxUnlimited, got.MaxBytes)
		}
	case "max":
		if !got.MaxKnown || !got.MaxUnlimited {
			t.Errorf("%s memory.max is \"max\" but its limit read as known=%t unlimited=%t, want unlimited", name, got.MaxKnown, got.MaxUnlimited)
		}
	default:
		limit, err := strconv.ParseUint(want.limit, 10, 64)
		if err != nil {
			t.Fatalf("fixture limit %q: %v", want.limit, err)
		}
		if !got.MaxKnown || got.MaxUnlimited || got.MaxBytes != limit {
			t.Errorf("%s memory.max is %d but its limit read as known=%t unlimited=%t bytes=%d", name, limit, got.MaxKnown, got.MaxUnlimited, got.MaxBytes)
		}
	}
	wantPressure := want.pressure == validPressureFile
	if got.PressureKnown != wantPressure {
		t.Errorf("%s pressure known = %t, want %t (memory.pressure %q)", name, got.PressureKnown, wantPressure, want.pressure)
	}
	if wantPressure && (got.PSISomeAvg10 != 1.5 || got.PSIFullTotalMicros != 400) {
		t.Errorf("%s pressure = some avg10 %.2f, full total %d; the file holds 1.50 and 400", name, got.PSISomeAvg10, got.PSIFullTotalMicros)
	}
	if got.PeakKnown {
		t.Errorf("%s has no memory.peak but its peak read as known (%d bytes), want unknown", name, got.PeakBytes)
	}
}

// assertAggregatesMatchFixture checks the Pod and workload rows the collector
// builds, and the namespace rows the client builds from them, against the sum
// of each one's containers in the tree.
func assertAggregatesMatchFixture(t *testing.T, ctx context.Context, reader *client.KubernetesAPIClient, fixture *pipelineFixture) {
	t.Helper()
	wantPods := map[string]memoryTotals{}
	wantWorkloads := map[string]memoryTotals{}
	wantNamespaces := map[string]memoryTotals{}
	for _, pod := range fixture.pods {
		var totals memoryTotals
		for _, container := range pod.containers {
			totals.add(container)
		}
		totals.pods = 1
		wantPods[pod.namespace+"/"+pod.name] = totals
		addTotals(wantWorkloads, pod.namespace+"/"+pod.workloadKind+"/"+pod.workloadName, totals)
		addTotals(wantNamespaces, pod.namespace, totals)
	}

	pods, err := reader.Pods(ctx)
	if err != nil {
		t.Fatalf("client Pods: %v", err)
	}
	gotPods := map[string]memoryTotals{}
	for _, pod := range pods {
		totals := totalsOf(pod.Memory)
		totals.pods, totals.containers = 1, len(pod.Containers)
		gotPods[pod.Namespace+"/"+pod.PodName] = totals
	}
	assertTotals(t, "Pod", gotPods, wantPods)

	workloads, err := reader.Workloads(ctx)
	if err != nil {
		t.Fatalf("client Workloads: %v", err)
	}
	gotWorkloads := map[string]memoryTotals{}
	for _, workload := range workloads {
		totals := totalsOf(workload.Memory)
		totals.pods = workload.PodCount
		for _, pod := range workload.Pods {
			totals.containers += len(pod.Containers)
		}
		gotWorkloads[workload.Namespace+"/"+workload.Kind+"/"+workload.Name] = totals
	}
	assertTotals(t, "workload", gotWorkloads, wantWorkloads)

	namespaces, err := reader.Namespaces(ctx)
	if err != nil {
		t.Fatalf("client Namespaces: %v", err)
	}
	gotNamespaces := map[string]memoryTotals{}
	for _, namespace := range namespaces {
		totals := totalsOf(namespace.Memory)
		totals.pods = namespace.PodCount
		// A namespace row carries no container list; compare what it reports.
		totals.containers = wantNamespaces[namespace.Namespace].containers
		gotNamespaces[namespace.Namespace] = totals
	}
	assertTotals(t, "namespace", gotNamespaces, wantNamespaces)
}

func addTotals(into map[string]memoryTotals, key string, totals memoryTotals) {
	sum := into[key]
	sum.total += totals.total
	sum.anon += totals.anon
	sum.file += totals.file
	sum.shmem += totals.shmem
	sum.pods += totals.pods
	sum.containers += totals.containers
	into[key] = sum
}

func assertTotals(t *testing.T, kind string, got, want map[string]memoryTotals) {
	t.Helper()
	for key, wantTotals := range want {
		gotTotals, found := got[key]
		if !found {
			t.Errorf("%s %s has containers in the tree but the client read no row for it", kind, key)
			continue
		}
		if gotTotals != wantTotals {
			t.Errorf("%s %s = %+v, the sum of its containers in the tree is %+v", kind, key, gotTotals, wantTotals)
		}
	}
	for key := range got {
		if _, expected := want[key]; !expected {
			t.Errorf("client read %s %s, which has no container in the tree: it is stale or invented", kind, key)
		}
	}
}
