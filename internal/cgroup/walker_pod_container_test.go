package cgroup

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	// A Kubernetes node that is itself a container (kind, k3d) shows up as
	// this scope when the agent sees the host's cgroup tree.
	testNodeScopeID = "1111111111111111111111111111111111111111111111111111111111111111"
	testInnerID     = "2222222222222222222222222222222222222222222222222222222222222222"
	testInnerPodUID = "87654321_4321_4321_4321_cba987654321"
)

func TestExtractContainerIDPrefersTheContainerBelowThePod(t *testing.T) {
	podSlice := "kubelet.slice/kubelet-kubepods.slice/kubelet-kubepods-pod" + testPodUIDSystemd + ".slice"
	container := "cri-containerd-" + testContainerID + ".scope"
	tests := map[string]struct{ path, want string }{
		"node scope above the Pod": {
			"docker-" + testNodeScopeID + ".scope/" + podSlice + "/" + container, testContainerID},
		"node scope alone": {
			"docker-" + testNodeScopeID + ".scope", testNodeScopeID},
		"slice between the node scope and the Pod": {
			"docker-" + testNodeScopeID + ".scope/kubelet.slice", testNodeScopeID},
		"Pod cgroup itself is not a container": {
			"docker-" + testNodeScopeID + ".scope/" + podSlice, ""},
		"sub-cgroup inside a container": {
			podSlice + "/" + container + "/system.slice/app.service", testContainerID},
		"Docker-in-Docker inside a container": {
			podSlice + "/" + container + "/docker/" + testInnerID, testContainerID},
		"Pod inside a container inside a Pod": {
			podSlice + "/" + container + "/kubelet.slice/kubelet-kubepods-pod" + testInnerPodUID + ".slice/cri-containerd-" + testInnerID + ".scope", testContainerID},
		"cgroupfs driver": {
			"kubepods/burstable/pod" + testPodUID + "/" + testContainerID, testContainerID},
		"upper-case ID is normalised": {
			"kubepods/pod" + testPodUID + "/" + strings.ToUpper(testContainerID), testContainerID},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := ExtractContainerIDFromPath(test.path); got != test.want {
				t.Fatalf("ExtractContainerIDFromPath(%q) = %q, want %q", test.path, got, test.want)
			}
		})
	}
}

func walkContainerIDs(t *testing.T, root string) []string {
	t.Helper()
	entries, err := (Walker{Root: root}).Walk()
	if err != nil {
		t.Fatalf("Walk returned error: %v", err)
	}
	ids := []string{}
	for _, entry := range entries {
		ids = append(ids, entry.PodUID+"/"+entry.ContainerID)
	}
	sort.Strings(ids)
	return ids
}

// The agent can see the host's cgroup tree with the node's own container
// scope above every Pod. Each Pod container must still be reported.
func TestWalkerReportsPodContainersBelowANodeScope(t *testing.T) {
	root := t.TempDir()
	node := filepath.Join(root, "system.slice", "docker-"+testNodeScopeID+".scope")
	writeCgroupFiles(t, node, 900, 800)
	writeCgroupFiles(t, filepath.Join(node, "kubelet.slice"), 700, 600)
	pod := filepath.Join(node, "kubelet.slice", "kubelet-kubepods.slice", "kubelet-kubepods-pod"+testPodUIDSystemd+".slice")
	writeCgroupFiles(t, pod, 500, 400)
	writeCgroupFiles(t, filepath.Join(pod, "cri-containerd-"+testContainerID+".scope"), 300, 200)
	writeCgroupFiles(t, filepath.Join(pod, "cri-containerd-"+testInnerID+".scope"), 100, 50)

	got := walkContainerIDs(t, root)
	want := []string{"/" + testNodeScopeID, testPodUID + "/" + testContainerID, testPodUID + "/" + testInnerID}
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("entries = %v, want %v", got, want)
	}
}

// Whatever a Pod's container runs inside itself is already charged to that
// container, including a nested runtime or a nested Kubernetes node.
func TestWalkerReportsAContainerOnceWhateverRunsInsideIt(t *testing.T) {
	root := t.TempDir()
	pod := filepath.Join(root, "kubepods", "burstable", "pod"+testPodUID)
	container := filepath.Join(pod, testContainerID)
	writeCgroupFiles(t, container, 900, 800)
	writeCgroupFiles(t, filepath.Join(container, "docker", testInnerID), 300, 200)
	writeCgroupFiles(t, filepath.Join(container, "kubelet.slice", "kubelet-kubepods-pod"+testInnerPodUID+".slice", "cri-containerd-"+testNodeScopeID+".scope"), 100, 50)

	got := walkContainerIDs(t, root)
	if len(got) != 1 || got[0] != testPodUID+"/"+testContainerID {
		t.Fatalf("entries = %v, want only %s/%s", got, testPodUID, testContainerID)
	}
}
