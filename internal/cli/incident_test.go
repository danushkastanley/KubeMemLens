package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestRedactIncidentRemovesRuntimeIdentifiers(t *testing.T) {
	bundle := api.IncidentBundle{
		Pods: []api.PodSnapshot{{
			PodUID:     "pod-uid",
			Context:    api.PodContext{Labels: map[string]string{"customer": "secret"}},
			Containers: []api.ContainerSnapshot{{PodUID: "pod-uid", ContainerID: "container-id", CgroupPath: "/sensitive/path", Context: api.ContainerContext{Labels: map[string]string{"customer": "secret"}}}},
		}},
		Histories: []api.PodHistory{{PodUID: "pod-uid"}},
	}
	redactIncident(&bundle)
	container := bundle.Pods[0].Containers[0]
	if bundle.Pods[0].PodUID != "" || container.PodUID != "" || container.ContainerID != "" || container.CgroupPath != "" || bundle.Histories[0].PodUID != "" || bundle.Pods[0].Context.Labels != nil || container.Context.Labels != nil {
		t.Fatalf("incident was not redacted: %#v", bundle)
	}
}

func TestWriteAndReadIncidentBundleUsesPrivateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "incident.json")
	bundle := api.IncidentBundle{
		SchemaVersion: api.CurrentIncidentSchemaVersion,
		CapturedAt:    time.Unix(1_700_000_000, 0).UTC(),
		ToolVersion:   "test",
		Redacted:      true,
		Pods: []api.PodSnapshot{{
			Namespace: "default", PodName: "api", Memory: model.MemoryBreakdown{TotalBytes: 123},
		}},
	}
	if err := writeIncidentBundle(io.Discard, path, false, bundle); err != nil {
		t.Fatalf("writeIncidentBundle returned error: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat incident: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("incident permissions = %o, want 600", info.Mode().Perm())
	}
	decoded, err := readIncidentBundle(path)
	if err != nil {
		t.Fatalf("readIncidentBundle returned error: %v", err)
	}
	if len(decoded.Pods) != 1 || decoded.Pods[0].Memory.TotalBytes != 123 {
		t.Fatalf("unexpected decoded incident: %#v", decoded)
	}
	if err := writeIncidentBundle(io.Discard, path, false, bundle); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second write error = %v, want already exists", err)
	}
}

func TestWriteIncidentBundleToStdout(t *testing.T) {
	output := &bytes.Buffer{}
	bundle := api.IncidentBundle{SchemaVersion: api.CurrentIncidentSchemaVersion, Redacted: true}
	if err := writeIncidentBundle(output, "-", false, bundle); err != nil {
		t.Fatalf("writeIncidentBundle returned error: %v", err)
	}
	if !strings.Contains(output.String(), `"schemaVersion": 1`) {
		t.Fatalf("unexpected JSON output: %s", output.String())
	}
}

func TestReadIncidentBundleRejectsUnknownAndTrailingJSON(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"unknown":  `{"schemaVersion":1,"unexpected":true}`,
		"trailing": `{"schemaVersion":1} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name+".json")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			if _, err := readIncidentBundle(path); err == nil {
				t.Fatal("readIncidentBundle returned nil error")
			}
		})
	}
}

func TestIncidentPodRequiresNamespaceAndName(t *testing.T) {
	bundle := api.IncidentBundle{Pods: []api.PodSnapshot{{Namespace: "default", PodName: "api"}}}
	if _, ok := incidentPod(bundle, "api"); ok {
		t.Fatal("incidentPod accepted a name without namespace")
	}
	if pod, ok := incidentPod(bundle, "default/api"); !ok || pod.PodName != "api" {
		t.Fatalf("incidentPod did not find Pod: %#v, %t", pod, ok)
	}
}
