package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/aggregate"
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/kube"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

const (
	podCgroupUID        = "12345678-1234-1234-1234-123456789abc"
	podCgroupUIDSystemd = "12345678_1234_1234_1234_123456789abc"
	otherPodUID         = "87654321-4321-4321-4321-cba987654321"
	podCgroupAppID      = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	podCgroupSidecarID  = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	podCgroupNodeID     = "1111111111111111111111111111111111111111111111111111111111111111"
	podCgroupInnerID    = "2222222222222222222222222222222222222222222222222222222222222222"
	podCgroupStaticHash = "c2df6c2eedc06b1195677a320a366f5e"
	podLimitBytes       = 256 << 20
)

var wantPodCgroup = model.PodCgroupMemory{CurrentBytes: 4096, CurrentKnown: true, MaxBytes: podLimitBytes, MaxKnown: true,
	LocalEventsKnown: true, LocalOOMEvents: 2, LocalOOMKillEvents: 1, LocalMaxEvents: 3}

// Each layout the walker supports must find the Pod cgroup by the component
// naming the Pod, not by the container's parent alone.
func TestScannerReadsThePodCgroupInEveryLayout(t *testing.T) {
	systemdPod := "kubepods-burstable-pod" + podCgroupUIDSystemd + ".slice"
	for name, layout := range map[string]struct {
		pod       []string
		container string
		cgroupUID string
		extra     func(t *testing.T, root, containerDir string)
	}{
		"systemd QoS slice":                    {pod: []string{"kubepods.slice", "kubepods-burstable.slice", systemdPod}, container: "cri-containerd-" + podCgroupAppID + ".scope"},
		"systemd Guaranteed":                   {pod: []string{"kubepods.slice", "kubepods-pod" + podCgroupUIDSystemd + ".slice"}, container: "cri-containerd-" + podCgroupAppID + ".scope"},
		"cgroupfs":                             {pod: []string{"kubepods", "burstable", "pod" + podCgroupUID}, container: podCgroupAppID},
		"CRI-O":                                {pod: []string{"kubepods.slice", "kubepods-burstable.slice", systemdPod}, container: "crio-" + podCgroupAppID + ".scope"},
		"static Pod by its configuration hash": {pod: []string{"kubepods", "pod" + podCgroupStaticHash}, container: podCgroupAppID, cgroupUID: podCgroupStaticHash},
		"kind node scope above the Pod": {
			pod:       []string{"system.slice", "docker-" + podCgroupNodeID + ".scope", "kubelet.slice", "kubelet-kubepods.slice", "kubelet-kubepods-pod" + podCgroupUIDSystemd + ".slice"},
			container: "cri-containerd-" + podCgroupAppID + ".scope",
			extra: func(t *testing.T, root, _ string) {
				writeScannerCgroup(t, filepath.Join(root, "system.slice", "docker-"+podCgroupNodeID+".scope"), 9000)
			},
		},
		"nested runtime inside the container": {
			pod: []string{"kubepods", "burstable", "pod" + podCgroupUID}, container: podCgroupAppID,
			extra: func(t *testing.T, _, containerDir string) {
				writeScannerCgroup(t, filepath.Join(containerDir, "docker", podCgroupInnerID), 300)
				writeScannerCgroup(t, filepath.Join(containerDir, "kubelet.slice", "kubelet-kubepods-pod87654321_4321_4321_4321_cba987654321.slice", "cri-containerd-"+podCgroupInnerID+".scope"), 100)
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := cgroupV2Root(t)
			podDir := filepath.Join(append([]string{root}, layout.pod...)...)
			writePodCgroup(t, podDir, "268435456")
			containerDir := filepath.Join(podDir, layout.container)
			writeScannerCgroup(t, containerDir, 1024)
			if layout.extra != nil {
				layout.extra(t, root, containerDir)
			}
			index := podCgroupIndex(podCgroupRef("app", podCgroupAppID))
			if layout.cgroupUID != "" {
				index.PodUIDs[layout.cgroupUID] = podCgroupUID
			}

			result := scanPodCgroups(t, root, index)
			app := scannedContainer(t, result, podCgroupAppID)
			if app.PodCgroup != wantPodCgroup {
				t.Fatalf("Pod cgroup = %#v, want %#v", app.PodCgroup, wantPodCgroup)
			}
		})
	}
}

// Kubernetes pod-level resources: the Pod cgroup has a finite memory.max while
// one container has none. Every mapped container of the Pod carries the same
// Pod cgroup; the sandbox and an unmapped cgroup carry nothing, and the Pod is
// no longer read as unlimited.
func TestScannerAttachesThePodCgroupToEveryMappedContainerOfThePod(t *testing.T) {
	root := cgroupV2Root(t)
	podDir := filepath.Join(root, "kubepods", "burstable", "pod"+podCgroupUID)
	writePodCgroup(t, podDir, "268435456")
	writeScannerCgroup(t, filepath.Join(podDir, podCgroupAppID), 1024)
	writeContainerLimit(t, filepath.Join(podDir, podCgroupAppID), "67108864")
	writeScannerCgroup(t, filepath.Join(podDir, podCgroupSidecarID), 2048)
	writeContainerLimit(t, filepath.Join(podDir, podCgroupSidecarID), "max")
	writeScannerCgroup(t, filepath.Join(podDir, sandboxCgroupID), 128)
	writePodCgroup(t, filepath.Join(root, "kubepods", "burstable", "pod"+otherPodUID), "max")
	writeScannerCgroup(t, filepath.Join(root, "kubepods", "burstable", "pod"+otherPodUID, secondCgroupID), 64)

	result := scanPodCgroups(t, root, podCgroupIndex(podCgroupRef("app", podCgroupAppID), podCgroupRef("sidecar", podCgroupSidecarID)))
	if result.InfrastructureCgroups != 1 || result.Mapped != 2 || result.Unmapped != 1 {
		t.Fatalf("mapped=%d unmapped=%d infrastructure=%d, want 2/1/1", result.Mapped, result.Unmapped, result.InfrastructureCgroups)
	}
	for _, id := range []string{podCgroupAppID, podCgroupSidecarID} {
		if got := scannedContainer(t, result, id).PodCgroup; got != wantPodCgroup {
			t.Fatalf("container %s Pod cgroup = %#v", id[:4], got)
		}
	}
	if scannedContainer(t, result, secondCgroupID).PodCgroup != (model.PodCgroupMemory{}) {
		t.Fatal("an unmapped cgroup was given a Pod cgroup")
	}
	pods := aggregate.Pods(result.Snapshot.Containers)
	if len(pods) != 1 || !pods[0].Memory.MaxUnlimited {
		t.Fatalf("pods = %#v, want one whose container sum is unlimited", pods)
	}
	limit := pods[0].EffectiveLimit()
	if limit.Source != api.PodLimitPodCgroup || limit.Unlimited || limit.LimitBytes != podLimitBytes || limit.UsedBytes != wantPodCgroup.CurrentBytes {
		t.Fatalf("effective limit = %#v, want the 256Mi Pod cgroup", limit)
	}
}

// A container gets the Pod cgroup only when the scanner can tell which Pod the
// directory belongs to and the cgroup path names the mapped Pod.
func TestScannerWithholdsThePodCgroupWhenThePodIsUncertain(t *testing.T) {
	for name, refs := range map[string][]kube.PodRef{
		"containers of one directory map to two Pods": {
			podCgroupRef("app", podCgroupAppID),
			{Namespace: "default", PodName: "other", PodUID: otherPodUID, ContainerName: "other", ContainerID: podCgroupSidecarID, Running: true},
		},
		"the cgroup path names another Pod": {
			{Namespace: "default", PodName: "api", PodUID: otherPodUID, ContainerName: "app", ContainerID: podCgroupAppID, Running: true},
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := cgroupV2Root(t)
			podDir := filepath.Join(root, "kubepods", "pod"+podCgroupUID)
			writePodCgroup(t, podDir, "268435456")
			for _, ref := range refs {
				writeScannerCgroup(t, filepath.Join(podDir, ref.ContainerID), 1024)
			}
			result := scanPodCgroups(t, root, podCgroupIndex(refs...))
			for _, container := range result.Snapshot.Containers {
				if container.PodCgroup != (model.PodCgroupMemory{}) {
					t.Fatalf("container %s was given an uncertain Pod cgroup", container.ContainerID[:4])
				}
			}
		})
	}
}

// An unreadable Pod cgroup is counted as a read error and leaves the Pod's
// cgroup unknown. It hides no container, so it must not become a walk error:
// a walk error withholds the node's whole snapshot.
func TestScannerCountsAnUnreadablePodCgroupWithoutWithholdingTheScan(t *testing.T) {
	root := cgroupV2Root(t)
	podDir := filepath.Join(root, "kubepods", "pod"+podCgroupUID)
	writePodCgroup(t, podDir, "not-a-number")
	writeScannerCgroup(t, filepath.Join(podDir, podCgroupAppID), 1024)

	result, err := (&Scanner{CgroupRoot: root, NodeName: "node-a"}).Scan(context.Background(), podCgroupIndex(podCgroupRef("app", podCgroupAppID)))
	if err != nil {
		t.Fatal(err)
	}
	if result.WalkError != nil || result.Snapshot.Environment.CgroupReadErrors != 1 {
		t.Fatalf("walk error = %v, read errors = %d; want one counted read error and no walk error", result.WalkError, result.Snapshot.Environment.CgroupReadErrors)
	}
	if app := scannedContainer(t, result, podCgroupAppID); app.PodCgroup != (model.PodCgroupMemory{}) {
		t.Fatalf("unreadable Pod cgroup reported as %#v", app.PodCgroup)
	}
}

func scanPodCgroups(t *testing.T, root string, index kube.PodIndex) ScanResult {
	t.Helper()
	result, err := (&Scanner{CgroupRoot: root, NodeName: "node-a"}).Scan(context.Background(), index)
	if err != nil || result.WalkError != nil {
		t.Fatalf("scan: %v, walk: %v", err, result.WalkError)
	}
	return result
}

func scannedContainer(t *testing.T, result ScanResult, id string) api.ContainerSnapshot {
	t.Helper()
	for _, container := range result.Snapshot.Containers {
		if container.ContainerID == id {
			return container
		}
	}
	t.Fatalf("container %s missing from %d scanned containers", id[:4], len(result.Snapshot.Containers))
	return api.ContainerSnapshot{}
}

func podCgroupRef(name, id string) kube.PodRef {
	return kube.PodRef{Namespace: "default", PodName: "api", PodUID: podCgroupUID, ContainerName: name, ContainerID: id, NodeName: "node-a", Running: true}
}

func podCgroupIndex(refs ...kube.PodRef) kube.PodIndex {
	index := kube.EmptyPodIndex()
	for _, ref := range refs {
		index.ByContainerID[ref.ContainerID] = ref
		index.ByPodUID[ref.PodUID] = append(index.ByPodUID[ref.PodUID], ref)
	}
	return index
}

func writePodCgroup(t *testing.T, dir, max string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string]string{
		"memory.current":      "4096\n",
		"memory.stat":         "anon 2048\nfile 1024\n",
		"memory.max":          max + "\n",
		"memory.events.local": "low 0\nhigh 0\nmax 3\noom 2\noom_kill 1\noom_group_kill 1\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func writeContainerLimit(t *testing.T, dir, max string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "memory.max"), []byte(max+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
