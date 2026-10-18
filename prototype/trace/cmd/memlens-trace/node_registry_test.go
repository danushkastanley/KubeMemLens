package main

import (
	"bytes"
	"encoding/json"
	"encoding/pem"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/prototype/trace/nodeprofile"
	"k8s.io/client-go/kubernetes/fake"
)

func registryFixture(t *testing.T) (string, endpointConfig) {
	t.Helper()
	server := httptest.NewTLSServer(nil)
	t.Cleanup(server.Close)
	directory := t.TempDir()
	caFile := filepath.Join(directory, "ca.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	profile := nodeprofile.Profile{NodeName: "node-one", NodeUID: "node-uid", Architecture: "amd64", KernelVersion: "6.12.0", RuntimeVersion: "containerd://2.2.0"}
	return filepath.Join(directory, "registry.json"), endpointConfig{NodeName: profile.NodeName, NodeUID: profile.NodeUID, URL: "https://node.invalid:9443", CAFile: caFile, CertificateSHA256: strings.Repeat("a", 64), Profile: &profile}
}

func writeRegistry(t *testing.T, path string, entries []endpointConfig) []byte {
	t.Helper()
	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRegistryCannotSilentlyDropPinnedProfiles(t *testing.T) {
	path, entry := registryFixture(t)
	writeRegistry(t, path, []endpointConfig{entry})
	registry, err := loadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	core := fake.NewSimpleClientset().CoreV1()
	if _, err := registry.resolver(core, "pinned"); err != nil {
		t.Fatal("valid pinned registry rejected", err)
	}
	if _, err := registry.resolver(core, "baseline"); err == nil {
		t.Fatal("profile checks silently dropped")
	}
	if _, err := registry.resolver(core, "unknown"); err == nil {
		t.Fatal("unknown profile policy accepted")
	}
	entry.Profile = nil
	writeRegistry(t, path, []endpointConfig{entry})
	registry, err = loadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.resolver(core, "baseline"); err != nil {
		t.Fatal("historical prototype configuration rejected", err)
	}
	if _, err := registry.resolver(core, "pinned"); err == nil {
		t.Fatal("pinned mode accepted missing profiles")
	}
}

func TestRegistryRejectsMixedAndConflictingIdentities(t *testing.T) {
	path, entry := registryFixture(t)
	other := entry
	other.NodeName, other.NodeUID, other.Profile = "node-two", "other-uid", nil
	writeRegistry(t, path, []endpointConfig{entry, other})
	if _, err := loadRegistry(path); err == nil {
		t.Fatal("partially pinned registry accepted")
	}
	entry.Profile.NodeUID = "different-uid"
	writeRegistry(t, path, []endpointConfig{entry})
	if _, err := loadRegistry(path); err == nil {
		t.Fatal("endpoint and profile identities diverged")
	}
}

func TestRegistryRejectsAmbiguousEncoding(t *testing.T) {
	path, entry := registryFixture(t)
	data := writeRegistry(t, path, []endpointConfig{entry})
	for _, invalid := range [][]byte{
		bytes.Replace(data, []byte(`"nodeUID":`), []byte(`"unknown":true,"nodeUID":`), 1),
		bytes.Replace(data, []byte(`"nodeUID":`), []byte(`"nodeUID":"duplicate","nodeUID":`), 1),
		bytes.Replace(data, []byte(`"nodeUID":`), []byte(`"NodeUID":`), 1),
		append(append([]byte(nil), data...), []byte(`[]`)...),
	} {
		if err := os.WriteFile(path, invalid, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadRegistry(path); err == nil {
			t.Fatal("ambiguous registry accepted")
		}
	}
}
