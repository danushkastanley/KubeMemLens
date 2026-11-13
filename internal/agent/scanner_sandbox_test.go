package agent

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/kube"
)

const (
	waitingPodUID     = "12345678-1234-1234-1234-123456789abc"
	sandboxCgroupID   = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	secondCgroupID    = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	exitedContainerID = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
)

// A Pod that cannot pull its image, or whose containers have all exited, still
// has a sandbox cgroup. It is not an unknown container and must not degrade
// the node for as long as the Pod exists.
func TestScannerClassifiesSandboxOfKnownPodWithNothingRunning(t *testing.T) {
	exited := kube.PodRef{Namespace: "jobs", PodName: "done", PodUID: waitingPodUID, ContainerName: "task", ContainerID: exitedContainerID}
	tests := map[string]func(*kube.PodIndex){
		"no container has started": func(*kube.PodIndex) {},
		"every container has exited": func(index *kube.PodIndex) {
			index.ByContainerID[exitedContainerID] = exited
			index.ByPodUID[waitingPodUID] = []kube.PodRef{exited}
		},
	}
	for name, prepare := range tests {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeScannerCgroup(t, filepath.Join(root, "kubepods", "pod"+waitingPodUID, sandboxCgroupID), 128)
			index := kube.EmptyPodIndex()
			index.PodUIDs[waitingPodUID] = waitingPodUID
			prepare(&index)

			scanner := &Scanner{CgroupRoot: root, NodeName: "node-a"}
			for scan := range 2 {
				result, err := scanner.Scan(context.Background(), index)
				if err != nil {
					t.Fatalf("scan %d: %v", scan, err)
				}
				if result.InfrastructureCgroups != 1 || result.Unmapped != 0 || len(result.Snapshot.Containers) != 0 {
					t.Fatalf("scan %d: infrastructure=%d unmapped=%d containers=%d", scan, result.InfrastructureCgroups, result.Unmapped, len(result.Snapshot.Containers))
				}
			}
		})
	}
}

// Only a cgroup that can be explained is hidden. Anything else under a Pod
// stays visible as unmapped evidence.
func TestScannerKeepsUnexplainedCgroupsOfIdlePodsVisible(t *testing.T) {
	tests := map[string]struct {
		cgroups []string
		known   bool
	}{
		"Pod is not known on this node":            {cgroups: []string{sandboxCgroupID}},
		"two cgroups, either could be the sandbox": {cgroups: []string{sandboxCgroupID, secondCgroupID}, known: true},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			for _, id := range test.cgroups {
				writeScannerCgroup(t, filepath.Join(root, "kubepods", "pod"+waitingPodUID, id), 128)
			}
			index := kube.EmptyPodIndex()
			if test.known {
				index.PodUIDs[waitingPodUID] = waitingPodUID
			}
			result, err := (&Scanner{CgroupRoot: root, NodeName: "node-a"}).Scan(context.Background(), index)
			if err != nil {
				t.Fatal(err)
			}
			if result.Unmapped != len(test.cgroups) || result.InfrastructureCgroups != 0 {
				t.Fatalf("unmapped=%d infrastructure=%d, want every cgroup to stay unmapped", result.Unmapped, result.InfrastructureCgroups)
			}
		})
	}
}
