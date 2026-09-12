package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"k8s.io/client-go/rest"
)

func TestReadUsesProductionSchemaAndNamespace(t *testing.T) {
	var paths []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Header.Get(api.SnapshotSchemaHeader) != strconv.Itoa(api.CurrentSnapshotSchemaVersion) {
			t.Error("current schema was not negotiated")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[],"metadata":{}}`))
	}))
	defer server.Close()
	config := &rest.Config{Host: server.URL, Transport: server.Client().Transport}
	var output bytes.Buffer
	err := read(context.Background(), config, options{namespace: "qualification", operation: "containers"}, &output)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "/apis/memory.kubememlens.io/v1alpha1/namespaces/qualification/containers" {
		t.Fatalf("unexpected request paths: %v", paths)
	}
	var value map[string]any
	if err := json.Unmarshal(output.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if len(value["items"].([]any)) != 0 {
		t.Fatal("unexpected containers")
	}
}

func TestUnverifiedTransportAndImplicitTargetsAreRejected(t *testing.T) {
	for _, config := range []*rest.Config{
		{Host: "http://fixture.invalid"},
		{Host: "https://user:password@fixture.invalid"},
		{Host: "https://fixture.invalid?credential=value"},
		{Host: "https://fixture.invalid#fragment"},
		{Host: "https://fixture.invalid", TLSClientConfig: rest.TLSClientConfig{Insecure: true}},
	} {
		if err := read(context.Background(), config, options{namespace: "qualification", operation: "status"}, &bytes.Buffer{}); err == nil {
			t.Fatal("unverified transport accepted")
		}
	}
	for _, opts := range []options{{}, {kubeconfig: "/private", context: "fixture", namespace: "qualification", operation: "arbitrary"}} {
		if err := run(context.Background(), opts, &bytes.Buffer{}); err == nil {
			t.Fatal("implicit target or unsupported operation accepted")
		}
	}
}
