package extension_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	pipelineNodeName = "node-a"
	pipelineNodeUID  = "6a1f0c52-7d1e-4c2b-9a51-0d3f2e4b5c61"
	// A Pod may carry far more labels than the collector accepts in one
	// container's context. The agent has to bound them before posting.
	pipelineManyLabels = 80
	validPressureFile  = "some avg10=1.50 avg60=0.75 avg300=0.25 total=1200\nfull avg10=0.50 avg60=0.25 avg300=0.10 total=400\n"
)

// fixtureContainer is one container cgroup on disk and the Pod status entry
// that names it. limit is the memory.max contents ("" leaves the file out);
// pressure is the memory.pressure contents ("" leaves the file out).
type fixtureContainer struct {
	name     string
	id       string
	current  uint64
	anon     uint64
	file     uint64
	shmem    uint64
	limit    string
	pressure string
}

type fixturePod struct {
	namespace    string
	name         string
	uid          string
	workloadKind string
	workloadName string
	labels       int
	// sandboxID is the Pod's CRI sandbox cgroup. Kubernetes does not list it,
	// so its memory must not be charged to any container or Pod.
	sandboxID  string
	containers []fixtureContainer
}

// pipelineFixture is a node's kubelet cgroup tree and the Pods the API server
// would report for that node. unmappedID is a container-looking cgroup no Pod
// names, as a container left behind by a deleted Pod would be.
type pipelineFixture struct {
	pods       []fixturePod
	unmappedID string
}

func newPipelineFixture() *pipelineFixture {
	return &pipelineFixture{
		unmappedID: hexID('e'),
		pods: []fixturePod{
			{
				namespace: "shop", name: "web-0", uid: "0b7f6a3e-1d2c-4e5f-8a9b-1c2d3e4f5a60",
				workloadKind: "StatefulSet", workloadName: "web", sandboxID: hexID('9'),
				containers: []fixtureContainer{
					{name: "app", id: hexID('a'), current: 300 << 20, anon: 200 << 20, file: 60 << 20, shmem: 8 << 20, limit: fmt.Sprint(512 << 20), pressure: validPressureFile},
					{name: "proxy", id: hexID('b'), current: 40 << 20, anon: 30 << 20, file: 4 << 20, shmem: 0, limit: "max"},
				},
			},
			{
				namespace: "shop", name: "web-1", uid: "1c8e7b4f-2e3d-4f60-9bac-2d3e4f5a6b71",
				workloadKind: "StatefulSet", workloadName: "web",
				containers: []fixtureContainer{
					// No memory.max and an unparseable memory.pressure: both stay
					// unknown, never zero.
					{name: "app", id: hexID('c'), current: 280 << 20, anon: 190 << 20, file: 50 << 20, shmem: 2 << 20, pressure: "not a pressure file\n"},
				},
			},
			{
				namespace: "batch", name: "report", uid: "2d9f8c50-3f4e-4071-8bcd-3e4f5a6b7c82",
				workloadKind: "Pod", workloadName: "report", labels: pipelineManyLabels,
				containers: []fixtureContainer{
					{name: "worker", id: hexID('d'), current: 96 << 20, anon: 64 << 20, file: 16 << 20, shmem: 1 << 20, limit: fmt.Sprint(128 << 20)},
				},
			},
		},
	}
}

func hexID(digit byte) string {
	id := make([]byte, 64)
	for index := range id {
		id[index] = digit
	}
	return string(id)
}

func (f *pipelineFixture) write(t *testing.T, root string) {
	t.Helper()
	// Every cgroup v2 root lists its controllers in this file.
	if err := os.WriteFile(filepath.Join(root, "cgroup.controllers"), []byte("cpu io memory pids\n"), 0o644); err != nil {
		t.Fatalf("write cgroup.controllers: %v", err)
	}
	for _, pod := range f.pods {
		for _, container := range pod.containers {
			writeFixtureCgroup(t, f.containerDir(root, pod, container.id), container)
		}
		if pod.sandboxID != "" {
			writeFixtureCgroup(t, f.containerDir(root, pod, pod.sandboxID), fixtureContainer{current: 1 << 20, anon: 1 << 20})
		}
	}
	orphan := filepath.Join(root, "kubepods", "burstable", "pod3e0a9d61-4a5f-4182-9cde-4f5a6b7c8d93", f.unmappedID)
	writeFixtureCgroup(t, orphan, fixtureContainer{current: 7 << 20, anon: 7 << 20})
}

func (f *pipelineFixture) containerDir(root string, pod fixturePod, id string) string {
	return filepath.Join(root, "kubepods", "burstable", "pod"+pod.uid, id)
}

// removePod deletes a Pod from the API view and its cgroups from the tree, as
// the kubelet does once the Pod is gone.
func (f *pipelineFixture) removePod(t *testing.T, root, namespace, name string) {
	t.Helper()
	for index, pod := range f.pods {
		if pod.namespace == namespace && pod.name == name {
			if err := os.RemoveAll(filepath.Join(root, "kubepods", "burstable", "pod"+pod.uid)); err != nil {
				t.Fatalf("remove Pod cgroup: %v", err)
			}
			f.pods = append(f.pods[:index], f.pods[index+1:]...)
			return
		}
	}
	t.Fatalf("fixture has no Pod %s/%s", namespace, name)
}

// setUsage rewrites one container's usage files, as the kernel would after the
// container's memory changed.
func (f *pipelineFixture) setUsage(t *testing.T, root, namespace, podName, containerName string, current, anon uint64) {
	t.Helper()
	for podIndex, pod := range f.pods {
		for containerIndex, container := range pod.containers {
			if pod.namespace == namespace && pod.name == podName && container.name == containerName {
				container.current, container.anon = current, anon
				f.pods[podIndex].containers[containerIndex] = container
				writeFixtureCgroup(t, f.containerDir(root, pod, container.id), container)
				return
			}
		}
	}
	t.Fatalf("fixture has no container %s/%s/%s", namespace, podName, containerName)
}

func writeFixtureCgroup(t *testing.T, dir string, container fixtureContainer) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create cgroup %s: %v", dir, err)
	}
	files := map[string]string{
		"memory.current": fmt.Sprintf("%d\n", container.current),
		"memory.stat": fmt.Sprintf("anon %d\nfile %d\nshmem %d\nkernel 1048576\nslab 524288\n",
			container.anon, container.file, container.shmem),
		"memory.events": "low 0\nhigh 0\nmax 0\noom 0\noom_kill 0\n",
	}
	if container.limit != "" {
		files["memory.max"] = container.limit + "\n"
	}
	if container.pressure != "" {
		files["memory.pressure"] = container.pressure
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

// apiPods returns the Pods the API server would list for the node, in the
// shape the agent's Pod index is built from.
func (f *pipelineFixture) apiPods() []corev1.Pod {
	pods := make([]corev1.Pod, 0, len(f.pods))
	for _, fixture := range f.pods {
		pod := corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: fixture.namespace, Name: fixture.name, UID: types.UID(fixture.uid),
				Labels: map[string]string{"app.kubernetes.io/name": fixture.workloadName},
			},
			Spec: corev1.PodSpec{NodeName: pipelineNodeName},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning, QOSClass: corev1.PodQOSBurstable,
			},
		}
		for index := 0; index < fixture.labels; index++ {
			pod.Labels[fmt.Sprintf("example.com/label-%03d", index)] = "value"
		}
		if fixture.workloadKind != "Pod" {
			controller := true
			pod.OwnerReferences = []metav1.OwnerReference{{
				APIVersion: "apps/v1", Kind: fixture.workloadKind, Name: fixture.workloadName,
				UID: types.UID(fixture.uid + "-owner"), Controller: &controller,
			}}
		}
		for _, container := range fixture.containers {
			pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{Name: container.name})
			pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, corev1.ContainerStatus{
				Name: container.name, ContainerID: "containerd://" + container.id, Ready: true,
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
			})
		}
		pods = append(pods, pod)
	}
	return pods
}
