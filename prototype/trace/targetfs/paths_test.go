package targetfs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
)

func workload() admission.Workload {
	return admission.Workload{Target: trace.TargetIdentity{Namespace: "tenant-a", PodName: "pod", PodUID: "11111111-2222-3333-4444-555555555555", ContainerName: "worker", ContainerID: strings.Repeat("a", 64), ContainerStartedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), NodeUID: "node-uid"}, NodeName: "node-one", QoS: "Burstable"}
}

func TestExactContainerdPathsPreserveFullPodAndContainerIDs(t *testing.T) {
	cases := []struct{ qos, systemd, filesystem string }{
		{"Guaranteed", "kubepods.slice/kubepods-pod11111111_2222_3333_4444_555555555555.slice/", "kubepods/pod11111111-2222-3333-4444-555555555555/"},
		{"Burstable", "kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod11111111_2222_3333_4444_555555555555.slice/", "kubepods/burstable/pod11111111-2222-3333-4444-555555555555/"},
		{"BestEffort", "kubepods.slice/kubepods-besteffort.slice/kubepods-besteffort-pod11111111_2222_3333_4444_555555555555.slice/", "kubepods/besteffort/pod11111111-2222-3333-4444-555555555555/"},
	}
	for _, tt := range cases {
		w := workload()
		w.QoS = tt.qos
		paths, err := candidates(Config{MountPoint: "/sys/fs/cgroup", KubeletRoot: "/"}, w)
		if err != nil || len(paths) != 2 {
			t.Fatal(err)
		}
		if paths[0] != tt.systemd+"cri-containerd-"+w.Target.ContainerID+".scope" || paths[1] != tt.filesystem+w.Target.ContainerID {
			t.Fatalf("wrong exact path for %s", tt.qos)
		}
	}
}

func TestPathInjectionPrefixesAndPreselectedCgroupsAreRejected(t *testing.T) {
	changes := []func(*admission.Workload){
		func(w *admission.Workload) { w.Target.PodUID = "../../other" },
		func(w *admission.Workload) { w.Target.PodUID = "11111111_2222_3333_4444_555555555555" },
		func(w *admission.Workload) { w.Target.ContainerID = "abcdef" },
		func(w *admission.Workload) { w.Target.ContainerID = strings.Repeat("a", 63) + "/" },
		func(w *admission.Workload) { w.Target.CgroupID = 123 },
		func(w *admission.Workload) { w.QoS = "../../other" },
	}
	for _, change := range changes {
		w := workload()
		change(&w)
		if _, err := candidates(Config{MountPoint: "/sys/fs/cgroup", KubeletRoot: "/"}, w); err == nil {
			t.Fatal("unsafe target produced a filesystem path")
		}
	}
}

func TestOrdinaryFilesystemsCannotSupplyACgroupIdentity(t *testing.T) {
	handle, err := Resolve(context.Background(), Config{MountPoint: t.TempDir(), KubeletRoot: "/"}, workload())
	if handle != nil || !errors.Is(err, admission.ErrUnavailable) {
		t.Fatal("ordinary directory supplied a kernel cgroup identity")
	}
}

func TestConfiguredKubeletRootChangesTheEntireSystemdHierarchy(t *testing.T) {
	w := workload()
	config := Config{MountPoint: "/sys/fs/cgroup", KubeletRoot: "/kubelet"}
	paths, err := candidates(config, w)
	if err != nil {
		t.Fatal(err)
	}
	if paths[0] != "kubelet.slice/kubelet-kubepods.slice/kubelet-kubepods-burstable.slice/kubelet-kubepods-burstable-pod11111111_2222_3333_4444_555555555555.slice/cri-containerd-"+w.Target.ContainerID+".scope" {
		t.Fatal("kubelet root did not participate in every slice name")
	}
	if paths[1] != "kubelet/kubepods/burstable/pod"+w.Target.PodUID+"/"+w.Target.ContainerID {
		t.Fatal("cgroupfs root missing")
	}
	for _, root := range []string{"", "relative", "/../kubelet", "/kube_let", "/a//b", "/a/b/c/d/e"} {
		config.KubeletRoot = root
		if _, err := candidates(config, w); err == nil {
			t.Fatal("unsafe or unbounded root accepted")
		}
	}
}
