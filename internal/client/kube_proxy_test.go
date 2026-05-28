package client

import (
	"errors"
	"strings"
	"testing"
)

func TestServiceProxyNameCandidates(t *testing.T) {
	got := serviceProxyNameCandidates("kube-memlens-collector", 8080)
	want := []string{
		"http:kube-memlens-collector:8080",
		"kube-memlens-collector:8080",
		"kube-memlens-collector",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("candidates = %#v, want %#v", got, want)
	}
}

func TestCollectorPathParts(t *testing.T) {
	got := collectorPathParts("/api/v1/pods")
	want := []string{"api", "v1", "pods"}
	if strings.Join(got, "/") != strings.Join(want, "/") {
		t.Fatalf("parts = %#v, want %#v", got, want)
	}
}

func TestServiceProxyRequestPath(t *testing.T) {
	got := serviceProxyRequestPath("kube-memlens", "http:kube-memlens-collector:8080", "/api/v1/pods")
	want := "/api/v1/namespaces/kube-memlens/services/http:kube-memlens-collector:8080/proxy/api/v1/pods"
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestKubeProxyJSONDecodeErrorIncludesEndpoint(t *testing.T) {
	var pods []struct{}
	err := decodeKubeProxyJSON("/api/v1/pods", []byte("not-json"), &pods)
	if err == nil {
		t.Fatal("decode returned nil error")
	}
	if !strings.Contains(err.Error(), "/api/v1/pods") {
		t.Fatalf("error = %q, want endpoint details", err.Error())
	}
}

func TestConnectionErrorForKubeProxyIncludesRemediation(t *testing.T) {
	err := ConnectionError(Options{
		Mode:               ConnectionModeKubeProxy,
		CollectorNamespace: "kube-memlens",
		CollectorService:   "kube-memlens-collector",
		CollectorPort:      8080,
	}, "", errors.New("forbidden"))

	for _, want := range []string{
		"Kubernetes API service proxy",
		"kubectl auth can-i get services/proxy -n kube-memlens",
		"Underlying error: forbidden",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want %q", err.Error(), want)
		}
	}
}
