package nodestats

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
	corev1 "k8s.io/api/core/v1"
)

func TestNodeEligibilityAndTargetValidation(t *testing.T) {
	for name, mutate := range map[string]func(*corev1.Node){
		"wrong-name":         func(n *corev1.Node) { n.Name = "another-node" },
		"missing-uid":        func(n *corev1.Node) { n.UID = "" },
		"invalid-uid":        func(n *corev1.Node) { n.UID = "private\nidentity" },
		"no-internal-ip":     func(n *corev1.Node) { n.Status.Addresses = nil },
		"unspecified-ip":     func(n *corev1.Node) { n.Status.Addresses[0].Address = "0.0.0.0" },
		"hostname":           func(n *corev1.Node) { n.Status.Addresses[0].Address = "private-target" },
		"invalid-port":       func(n *corev1.Node) { n.Status.DaemonEndpoints.KubeletEndpoint.Port = 70000 },
		"missing-port":       func(n *corev1.Node) { n.Status.DaemonEndpoints.KubeletEndpoint.Port = 0 },
		"too-many-addresses": func(n *corev1.Node) { n.Status.Addresses = make([]corev1.NodeAddress, 65) },
		"duplicate-pressure": func(n *corev1.Node) { n.Status.Conditions = append(n.Status.Conditions, n.Status.Conditions[0]) },
	} {
		t.Run(name, func(t *testing.T) {
			node := testNode(10250)
			mutate(&node)
			data, _ := json.Marshal(node)
			_, err := decodeNode(t.Context(), data, "node-a", sampleNow)
			assertReason(t, err, nodecontext.InvalidTarget)
		})
	}
	for _, version := range []string{"v1.35.5", "v1.38.0", "v2.0.0", "invalid"} {
		node := testNode(10250)
		node.Status.NodeInfo.KubeletVersion = version
		data, _ := json.Marshal(node)
		_, err := decodeNode(t.Context(), data, "node-a", sampleNow)
		assertReason(t, err, nodecontext.Unsupported)
	}
	node := testNode(10250)
	node.Status.NodeInfo.OperatingSystem = "windows"
	data, _ := json.Marshal(node)
	_, err := decodeNode(t.Context(), data, "node-a", sampleNow)
	assertReason(t, err, nodecontext.Unsupported)
}

func TestHostileNodeResourceQuantitiesAreBounded(t *testing.T) {
	data, _ := json.Marshal(testNode(10250))
	for _, quantity := range []string{"-1", "1e999999999", "999999999999999999999999999999999E", "invalid"} {
		body := strings.Replace(string(data), `"memory":"8Gi"`, `"memory":"`+quantity+`"`, 1)
		if _, err := decodeNode(t.Context(), []byte(body), "node-a", sampleNow); err == nil {
			t.Fatalf("accepted %s", quantity)
		}
	}
	badName := strings.ReplaceAll(string(data), "hugepages-2Mi", "hugepages-private-resource")
	if _, err := decodeNode(t.Context(), []byte(badName), "node-a", sampleNow); err == nil {
		t.Fatal("accepted arbitrary resource name")
	}
}

func TestNodeResponseOrderingDoesNotChangeTheTarget(t *testing.T) {
	node := testNode(10250)
	node.Status.Addresses = []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "::1"}, {Type: corev1.NodeInternalIP, Address: "127.0.0.1"}}
	data, _ := json.Marshal(node)
	first, err := decodeNode(t.Context(), data, "node-a", sampleNow)
	if err != nil {
		t.Fatal(err)
	}
	node.Status.Addresses[0], node.Status.Addresses[1] = node.Status.Addresses[1], node.Status.Addresses[0]
	data, _ = json.Marshal(node)
	second, err := decodeNode(t.Context(), data, "node-a", sampleNow)
	if err != nil || first.endpoint() != second.endpoint() || first.endpoint() != "https://127.0.0.1:10250/stats/summary" {
		t.Fatalf("targets=%s %s %v", first.endpoint(), second.endpoint(), err)
	}
}

func FuzzNode(f *testing.F) {
	data, _ := json.Marshal(testNode(10250))
	f.Add(data)
	f.Fuzz(func(t *testing.T, data []byte) {
		value, err := decodeNode(t.Context(), data, "node-a", sampleNow)
		if err == nil && len(value.context.Hugepages) > nodecontext.MaxHugepages {
			t.Fatal("unbounded hugepage resources")
		}
	})
}
