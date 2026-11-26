package targetfs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"golang.org/x/sys/unix"
)

// Explicit local qualification only. The input contains identities of synthetic
// Pods in an owned disposable cluster and is never retained in test output.
func TestLiveExactBinding(t *testing.T) {
	if os.Getenv("KML_TARGETFS_LIVE") != "owned-local-kind-node" {
		t.Skip("requires owned local Kubernetes fixture")
	}
	var filesystem unix.Statfs_t
	if unix.Statfs("/sys/fs/cgroup", &filesystem) != nil || filesystem.Type != unix.CGROUP2_SUPER_MAGIC || filesystem.Flags&unix.ST_RDONLY == 0 {
		t.Fatal("fixture cgroup mount is not read-only cgroup v2")
	}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatal("process security state unavailable")
	}
	fields := map[string]string{}
	for _, line := range strings.Split(string(status), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok {
			fields[key] = strings.TrimSpace(value)
		}
	}
	for _, name := range []string{"CapEff", "CapPrm", "CapBnd", "CapInh", "CapAmb"} {
		bits, err := strconv.ParseUint(fields[name], 16, 64)
		if err != nil || bits != 0 {
			t.Fatal("live binding fixture has unexpected capabilities")
		}
	}
	if fields["NoNewPrivs"] != "1" || fields["Seccomp"] != "2" {
		t.Fatal("live binding fixture lacks confinement")
	}
	file, err := os.Open("/input/target.json")
	if err != nil {
		t.Fatal("live target input unavailable")
	}
	defer file.Close()
	var input struct {
		Namespace, Pod, PodUID, Container, ContainerID, NodeUID, NodeName, QoS, OtherContainerID string
		StartedAt                                                                                time.Time
	}
	decoder := json.NewDecoder(io.LimitReader(file, 4097))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil {
		t.Fatal("invalid live target input")
	}
	if len(input.OtherContainerID) != 64 || strings.Trim(input.OtherContainerID, "0123456789abcdef") != "" || input.OtherContainerID == input.ContainerID {
		t.Fatal("distinct neighbour fixture is missing")
	}
	w := admission.Workload{Target: trace.TargetIdentity{Namespace: input.Namespace, PodName: input.Pod, PodUID: input.PodUID, ContainerName: input.Container, ContainerID: input.ContainerID, ContainerStartedAt: input.StartedAt, NodeUID: input.NodeUID}, NodeName: input.NodeName, QoS: input.QoS}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	handle, err := Resolve(ctx, Config{MountPoint: "/sys/fs/cgroup", KubeletRoot: os.Getenv("KML_KUBELET_ROOT")}, w)
	if err != nil {
		t.Fatalf("live cgroup binding: %v", err)
	}
	defer handle.Close()
	if handle.Target().CgroupID == 0 || handle.Target().ContainerID != input.ContainerID {
		t.Fatal("live identity was not retained")
	}
	if err := handle.Check(ctx); err != nil {
		t.Fatalf("repeat binding check: %v", err)
	}
	other := w
	other.Target.ContainerID = input.OtherContainerID
	if mismatch, err := Resolve(ctx, Config{MountPoint: "/sys/fs/cgroup", KubeletRoot: os.Getenv("KML_KUBELET_ROOT")}, other); err == nil {
		mismatch.Close()
		t.Fatal("other Pod's container ID matched selected Pod")
	}
	if err := handle.Close(); err != nil {
		t.Fatal("binding close failed")
	}
	if err := handle.Check(ctx); !errors.Is(err, admission.ErrExpired) {
		t.Fatal("closed binding remained usable")
	}
	fmt.Println(`BINDING_PROOF={"outcome":"passed","capabilities":0,"readOnlyCgroupMount":true,"fullIdentity":true,"otherContainerRejected":true,"closedBindingRejected":true}`)
}

func TestZeroHandleCannotCloseAnUnownedDescriptor(t *testing.T) {
	h := &handle{}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	if err := h.Check(context.Background()); !errors.Is(err, admission.ErrExpired) {
		t.Fatal("zero handle was treated as owned")
	}
}

func TestHandleDoesNotExposeInternalPathsThroughFormatting(t *testing.T) {
	var h Handle = &handle{root: "private-root", path: "private-container"}
	if got := fmt.Sprintf("%+v", h); got != "[trace cgroup binding]" {
		t.Fatal("binding formatting exposes internals")
	}
	if _, err := json.Marshal(h); err == nil {
		t.Fatal("binding accepted ordinary JSON retention")
	}
}
