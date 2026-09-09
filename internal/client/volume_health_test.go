package client

import "testing"

func TestVolumeHealthConnectionAndScope(t *testing.T) {
	for _, opts := range []Options{{Mode: ConnectionModeHTTP, CollectorURL: "http://example.invalid"}, {Mode: ConnectionModeKubeProxy}, {ReadScope: AllNamespacesScope()}, {Mode: "invalid"}} {
		if _, err := NewVolumeHealthSource(opts); err == nil {
			t.Fatal("non-Kubernetes or unbounded scope accepted")
		}
	}
	if _, err := NewVolumeHealthSource(Options{Mode: ConnectionModeKubernetesAPI, Kubeconfig: writeTestKubeconfig(t)}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewVolumeHealthSource(Options{Mode: ConnectionModeKubernetesAPI, Kubeconfig: "/missing-kubeconfig"}); err == nil {
		t.Fatal("missing configuration accepted")
	}
}
