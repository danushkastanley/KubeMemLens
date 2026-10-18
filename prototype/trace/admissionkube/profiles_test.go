package admissionkube

import (
	"context"
	"testing"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/prototype/trace/nodeprofile"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func pinnedNodeFixture() (*corev1.Node, nodeprofile.Profile) {
	node := nodeFixture()
	node.Status.NodeInfo = corev1.NodeSystemInfo{OperatingSystem: "linux", Architecture: "amd64", KernelVersion: "6.12.0", ContainerRuntimeVersion: "containerd://2.2.0"}
	return node, nodeprofile.Profile{NodeName: node.Name, NodeUID: string(node.UID), Architecture: "amd64", KernelVersion: "6.12.0", RuntimeVersion: "containerd://2.2.0"}
}

func TestPinnedResolverUsesOwnedProfilesAndExistingFreshReads(t *testing.T) {
	node, profile := pinnedNodeFixture()
	client := fake.NewSimpleClientset(podFixture(), node)
	profiles := []nodeprofile.Profile{profile}
	resolver, err := NewPinnedResolver(client.CoreV1(), profiles)
	if err != nil {
		t.Fatal(err)
	}
	profiles[0].KernelVersion = "changed-by-caller"
	ctx := context.Background()
	workload, err := resolver.Resolve(ctx, input(t))
	if err != nil || len(client.Actions()) != 2 {
		t.Fatal("profile aliasing or unexpected acquisition requests", err)
	}
	if err := resolver.Revalidate(ctx, workload); err != nil || len(client.Actions()) != 4 {
		t.Fatal("profile revalidation did not reuse the fresh Node read", err)
	}
	target := workload.Target
	target.CgroupID = 123 // Supplied by the node binding before OOM context reads.
	if _, err := resolver.SampleOOMContext(ctx, target); err != nil || len(client.Actions()) != 6 {
		t.Fatal("OOM context bypassed the same profile/read boundary", err)
	}
}

func TestProfileDriftRejectsAdmissionRevalidationAndOOMContext(t *testing.T) {
	for name, mutate := range map[string]func(*corev1.Node){
		"kernel":           func(n *corev1.Node) { n.Status.NodeInfo.KernelVersion = "6.13.0" },
		"runtime":          func(n *corev1.Node) { n.Status.NodeInfo.ContainerRuntimeVersion = "containerd://2.2.1" },
		"architecture":     func(n *corev1.Node) { n.Status.NodeInfo.Architecture = "arm64" },
		"operating-system": func(n *corev1.Node) { n.Status.NodeInfo.OperatingSystem = "windows" },
		"replacement-node": func(n *corev1.Node) { n.UID = "replacement" },
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			node, profile := pinnedNodeFixture()
			client := fake.NewSimpleClientset(podFixture(), node)
			resolver, err := NewPinnedResolver(client.CoreV1(), []nodeprofile.Profile{profile})
			if err != nil {
				t.Fatal(err)
			}
			workload, err := resolver.Resolve(ctx, input(t))
			if err != nil {
				t.Fatal(err)
			}
			mutate(node)
			if _, err := client.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, err := resolver.Resolve(ctx, input(t)); err != admission.ErrTargetChanged {
				t.Fatal("new trace accepted a changed profile", err)
			}
			if err := resolver.Revalidate(ctx, workload); err != admission.ErrTargetChanged {
				t.Fatal("active trace retained a changed profile", err)
			}
			target := workload.Target
			target.CgroupID = 123
			if _, err := resolver.SampleOOMContext(ctx, target); err != admission.ErrTargetChanged {
				t.Fatal("OOM context accepted a changed profile", err)
			}
		})
	}
}

func TestPinnedResolverRejectsMissingOrAmbiguousProfiles(t *testing.T) {
	node, profile := pinnedNodeFixture()
	core := fake.NewSimpleClientset(node).CoreV1()
	for _, profiles := range [][]nodeprofile.Profile{nil, {{}}, {profile, profile}} {
		if _, err := NewPinnedResolver(core, profiles); err != admission.ErrInvalidRequest {
			t.Fatal("invalid pinned registry accepted")
		}
	}
	if _, err := NewPinnedResolver(nil, []nodeprofile.Profile{profile}); err != admission.ErrInvalidRequest {
		t.Fatal("missing current-state reader accepted")
	}
}
