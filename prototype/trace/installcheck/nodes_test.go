package installcheck

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func specFixture() Spec {
	return Spec{SchemaVersion: 1, Namespace: "trace-admin", APIServiceName: "trial-api", NodeServicePrefix: "trial-node", PolicySHA256: strings.Repeat("a", 64), ControlCertificateSHA256: strings.Repeat("b", 64), APICABundle: []byte("fixture CA"), Nodes: []Node{{ID: "one", Name: "node-one", UID: "node-one-uid", Architecture: "amd64", KernelVersion: "6.12.0", RuntimeVersion: "containerd://2.2.0", TLSSecret: "node-one-tls", CertificateSHA256: strings.Repeat("c", 64), KubeletCgroupRoot: "/kubelet"}}}
}

type nodeReaderFunc func(context.Context, string, metav1.GetOptions) (*corev1.Node, error)

func (f nodeReaderFunc) Get(ctx context.Context, name string, options metav1.GetOptions) (*corev1.Node, error) {
	return f(ctx, name, options)
}

func nodeFixture(spec Spec) *corev1.Node {
	n := spec.Nodes[0]
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: n.Name, UID: types.UID(n.UID)}, Status: corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{OperatingSystem: "linux", Architecture: n.Architecture, KernelVersion: n.KernelVersion, ContainerRuntimeVersion: n.RuntimeVersion}, Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}}}
}

func TestCurrentNodeProfileAndCancellation(t *testing.T) {
	spec := specFixture()
	calls := 0
	reader := nodeReaderFunc(func(ctx context.Context, name string, options metav1.GetOptions) (*corev1.Node, error) {
		calls++
		if name != spec.Nodes[0].Name || options.ResourceVersion != "" {
			t.Fatal("node selection changed or read used a cached resource version")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("unbounded API read")
		}
		return nodeFixture(spec), nil
	})
	if err := CheckNodes(context.Background(), reader, spec); err != nil || calls != 1 {
		t.Fatal("current profile rejected", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := CheckNodes(ctx, reader, spec); err != ErrUnavailable || calls != 1 {
		t.Fatal("cancelled verification accepted")
	}
}

func TestUnsupportedAndChangedNodesFailClosed(t *testing.T) {
	for name, mutate := range map[string]func(*corev1.Node){
		"renamed":          func(n *corev1.Node) { n.Name = "other" },
		"recreated":        func(n *corev1.Node) { n.UID = "replacement" },
		"architecture":     func(n *corev1.Node) { n.Status.NodeInfo.Architecture = "arm64" },
		"kernel":           func(n *corev1.Node) { n.Status.NodeInfo.KernelVersion = "6.13.0" },
		"runtime":          func(n *corev1.Node) { n.Status.NodeInfo.ContainerRuntimeVersion = "containerd://2.2.1" },
		"operating-system": func(n *corev1.Node) { n.Status.NodeInfo.OperatingSystem = "windows" },
		"cordoned":         func(n *corev1.Node) { n.Spec.Unschedulable = true },
		"deleting":         func(n *corev1.Node) { at := metav1.Now(); n.DeletionTimestamp = &at },
		"not-ready":        func(n *corev1.Node) { n.Status.Conditions[0].Status = corev1.ConditionFalse },
		"missing-ready":    func(n *corev1.Node) { n.Status.Conditions = nil },
		"duplicate-ready":  func(n *corev1.Node) { n.Status.Conditions = append(n.Status.Conditions, n.Status.Conditions[0]) },
		"contradictory-ready": func(n *corev1.Node) {
			n.Status.Conditions = append(n.Status.Conditions, corev1.NodeCondition{Type: corev1.NodeReady, Status: corev1.ConditionFalse})
		},
	} {
		t.Run(name, func(t *testing.T) {
			spec := specFixture()
			node := nodeFixture(spec)
			mutate(node)
			reader := nodeReaderFunc(func(context.Context, string, metav1.GetOptions) (*corev1.Node, error) { return node, nil })
			if err := CheckNodes(context.Background(), reader, spec); err == nil {
				t.Fatal("changed or unsupported Node accepted")
			}
		})
	}
}

func TestAPIErrorDoesNotExposePrivateResponse(t *testing.T) {
	reader := nodeReaderFunc(func(context.Context, string, metav1.GetOptions) (*corev1.Node, error) {
		return nil, errors.New("private server address and response")
	})
	if err := CheckNodes(context.Background(), reader, specFixture()); err != ErrUnavailable {
		t.Fatal("API error was not reduced to unavailable")
	}
}
