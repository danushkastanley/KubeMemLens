package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func fixtureDocument() map[string]any {
	return map[string]any{"schemaVersion": 1, "kubeconfig": "private-kubeconfig", "context": "kind-owned",
		"runID": strings.Repeat("a", 32), "namespace": "kml-isolation-a", "pod": "target", "container": "worker",
		"podUID": "private-pod-uid", "containerID": strings.Repeat("b", 64), "containerStartedAt": "2026-09-30T00:00:00Z",
		"nodeName": "owned-node", "kind": "files", "confirmedPaths": true, "durationSeconds": 20, "maxEvents": 10000}
}
func writeFixture(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "configuration.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestConfigurationFreezesBoundedFixtureAndMasksIdentity(t *testing.T) {
	data, _ := json.Marshal(fixtureDocument())
	c, err := readConfiguration(writeFixture(t, data))
	if err != nil || c.DurationSeconds != 20 || !c.ConfirmedPaths {
		t.Fatal("valid fixture rejected", err)
	}
	if strings.Contains(fmt.Sprintf("%+v", c), "private-pod-uid") {
		t.Fatal("configuration formatted private identity")
	}
	if _, err := json.Marshal(c); err == nil {
		t.Fatal("configuration accidentally serialised")
	}
}
func TestConfigurationRejectsUnboundedOrNonFixtureSelections(t *testing.T) {
	for key, value := range map[string]any{"schemaVersion": 2, "context": "production", "namespace": "default", "runID": "unknown",
		"podUID": "", "containerID": "short", "containerStartedAt": "0001-01-01T00:00:00Z", "nodeName": "invalid/name",
		"kind": "unknown", "durationSeconds": 31, "maxEvents": 10001} {
		t.Run(key, func(t *testing.T) {
			doc := fixtureDocument()
			doc[key] = value
			data, _ := json.Marshal(doc)
			if _, err := readConfiguration(writeFixture(t, data)); err == nil {
				t.Fatal("unsafe fixture configuration accepted")
			}
		})
	}
	for _, kind := range []string{"cache", "oom"} {
		doc := fixtureDocument()
		doc["kind"] = kind
		data, _ := json.Marshal(doc)
		if _, err := readConfiguration(writeFixture(t, data)); err == nil {
			t.Fatal("inapplicable path consent accepted")
		}
	}
}
func TestConfigurationRejectsDuplicateUnknownOversizedAndNonRegularInputs(t *testing.T) {
	valid, _ := json.Marshal(fixtureDocument())
	for _, data := range [][]byte{append([]byte(`{"schemaVersion":1,`), valid[1:]...), append([]byte(`{"unknown":1,`), valid[1:]...), []byte(strings.Repeat(" ", 4097))} {
		if _, err := readConfiguration(writeFixture(t, data)); err == nil {
			t.Fatal("invalid JSON configuration accepted")
		}
	}
	directory := t.TempDir()
	fifo := filepath.Join(directory, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{directory, fifo, filepath.Join(directory, "missing")} {
		if _, err := readConfiguration(path); err == nil {
			t.Fatal("non-file configuration accepted")
		}
	}
}
func localConfig() *rest.Config {
	return &rest.Config{Host: "https://127.0.0.1:6443", BearerToken: "fixture-token", TLSClientConfig: rest.TLSClientConfig{CAData: []byte("fixture trust")}}
}
func TestQualificationRejectsCloudAndAlternateCredentials(t *testing.T) {
	if fixtureCredentials(localConfig(), nil) != nil {
		t.Fatal("local token fixture rejected")
	}
	changes := []func(*rest.Config){
		func(c *rest.Config) { c.Host = "https://api.example.com" }, func(c *rest.Config) { c.Host = "http://127.0.0.1" },
		func(c *rest.Config) { c.Host = "https://user@localhost" }, func(c *rest.Config) { c.Host = "https://localhost/path" },
		func(c *rest.Config) { c.Insecure = true }, func(c *rest.Config) { c.CAData = nil }, func(c *rest.Config) { c.CAFile = "private-ca" },
		func(c *rest.Config) { c.BearerToken = "" }, func(c *rest.Config) { c.BearerTokenFile = "private-token" },
		func(c *rest.Config) { c.CertData = []byte("admin certificate") }, func(c *rest.Config) { c.Username = "admin" },
		func(c *rest.Config) { c.Impersonate.UserName = "admin" },
		func(c *rest.Config) { c.ExecProvider = &clientcmdapi.ExecConfig{Command: "credential-helper"} },
		func(c *rest.Config) { c.AuthProvider = &clientcmdapi.AuthProviderConfig{Name: "provider"} },
	}
	for _, change := range changes {
		c := localConfig()
		change(c)
		if fixtureCredentials(c, nil) == nil {
			t.Fatal("remote endpoint or alternate credential accepted")
		}
	}
}
