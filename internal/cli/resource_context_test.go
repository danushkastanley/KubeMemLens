package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/collector"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func resourceCLIPod() api.PodSnapshot {
	resources := model.PodMemoryResources{
		Configured: model.MemoryResourceBudget{Request: model.ResourceValue{Bytes: 192 << 20, Known: true}, Limit: model.ResourceValue{Bytes: 384 << 20, Known: true}},
		Pending:    model.ResizeObservation{State: model.ResizeDeferred, Source: model.ResizePodCondition},
	}
	container := api.ContainerSnapshot{
		Namespace: "tenant-a", PodName: "app", PodUID: "private-uid", ContainerName: "worker", ContainerID: "private-id", CgroupPath: "/private-path",
		NodeName: "node-a", CapturedAt: time.Now().UTC(),
		Context: api.ContainerContext{MemoryRequestKnown: true, MemoryRequestBytes: 128 << 20, Resources: model.ContainerMemoryResources{Pod: resources}},
	}
	return api.PodSnapshot{Namespace: "tenant-a", PodName: "app", PodUID: "private-uid", Containers: []api.ContainerSnapshot{container}, Context: api.PodContext{Resources: resources, MemoryRequestBytes: 128 << 20, MemoryRequestContainers: 1}}
}

func TestResourceExplanationVersionAndHumanContext(t *testing.T) {
	pod := resourceCLIPod()
	document := podExplanationDocument(pod)
	if document.SchemaVersion != api.CurrentExplanationSchemaVersion || document.Kubernetes.Resources != pod.Context.Resources ||
		document.Kubernetes.EffectiveResources.Request.Bytes != 192<<20 || len(document.Containers) != 1 || document.Containers[0].Resources.IsZero() {
		t.Fatalf("machine explanation lost resource context: %+v", document)
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private-") {
		t.Fatal("machine resource explanation leaked identifiers")
	}
	var output bytes.Buffer
	printPodContext(&output, pod)
	if !strings.Contains(output.String(), "192Mi (Pod configuration)") || !strings.Contains(output.String(), "Resize allocation: deferred") {
		t.Fatalf("human context lost source or resize state: %s", output.String())
	}
	legacy := api.LegacyPodSnapshot(pod)
	if doc := podExplanationDocument(legacy); doc.SchemaVersion != api.CurrentExplanationSchemaVersion || doc.Kubernetes.EffectiveResources != nil {
		t.Fatal("legacy explanation contract changed")
	}
}

func TestCaptureCommandWritesCurrentAndExplicitLegacyResources(t *testing.T) {
	pod := resourceCLIPod()
	store := collector.NewStore()
	if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{NodeName: "node-a", CapturedAt: pod.Containers[0].CapturedAt, Containers: pod.Containers}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(collector.NewReadHandlerWithOptions(store, collector.DefaultHandlerOptions(time.Minute)))
	defer server.Close()
	for _, schema := range []string{"0", "1", "2"} {
		t.Run(schema, func(t *testing.T) {
			var output bytes.Buffer
			command := newCaptureCommand(func() client.Options {
				return client.Options{Mode: client.ConnectionModeHTTP, CollectorURL: server.URL}
			})
			command.SetOut(&output)
			command.SetErr(io.Discard)
			command.SetArgs([]string{"--namespace", "tenant-a", "--pod", "app", "--output", "-", "--schema-version", schema})
			if err := command.Execute(); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(output.String(), "private-") {
				t.Fatal("capture resource context leaked identifiers")
			}
			path := filepath.Join(t.TempDir(), "capture.json")
			if err := os.WriteFile(path, output.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			bundle, err := readIncidentBundle(path)
			if err != nil {
				t.Fatal(err)
			}
			if len(bundle.Pods) != 1 {
				t.Fatal("capture lost the selected Pod")
			}
			var replayOutput bytes.Buffer
			replay := newReplayCommand()
			replay.SetOut(&replayOutput)
			replay.SetArgs([]string{path, "--pod", "tenant-a/app"})
			if err := replay.Execute(); err != nil {
				t.Fatal(err)
			}
			if schema == "1" {
				if !strings.Contains(replayOutput.String(), "omitted for incident schema 1") {
					t.Fatal("replay hid the omitted resource context")
				}
				if bundle.SchemaVersion != 1 || api.PodHasResourceContext(bundle.Pods[0]) || !strings.Contains(strings.Join(bundle.Caveats, " "), "omitted") {
					t.Fatal("legacy capture did not explicitly omit unsupported context")
				}
				return
			}
			if bundle.SchemaVersion != 2 || bundle.Pods[0].Context.Resources.Configured.Limit.Bytes != 384<<20 {
				t.Fatal("capture/replay lost the configured Pod limit")
			}
		})
	}
}

func TestReplayRejectsResourceContextMasqueradingAsLegacy(t *testing.T) {
	bundle := api.IncidentBundle{SchemaVersion: 1, Pods: []api.PodSnapshot{resourceCLIPod()}}
	encoded, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "mislabeled.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readIncidentBundle(path); err == nil || !strings.Contains(err.Error(), "requires incident schema 2") {
		t.Fatalf("mislabeled capture accepted: %v", err)
	}
}

func TestResourceComparisonPreservesReplicaAndContainerContributions(t *testing.T) {
	before := resourceCLIPod()
	before.Context.WorkloadKind, before.Context.WorkloadName = "Deployment", "service"
	after := before
	after.Containers = append([]api.ContainerSnapshot(nil), before.Containers...)
	after.Context.Resources.Configured.Limit.Bytes = 512 << 20
	after.Containers[0].Context.MemoryRequestBytes = 160 << 20
	var output bytes.Buffer
	printPodComparison(&output, "comparison", before, after, time.Minute)
	for _, want := range []string{"Pod configured limit: 384Mi -> 512Mi", "Container worker configured request: 128Mi -> 160Mi"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("missing %q in %s", want, output.String())
		}
	}
	output.Reset()
	printWorkloadResourceComparison(&output, api.IncidentBundle{Pods: []api.PodSnapshot{before}}, api.IncidentBundle{Pods: []api.PodSnapshot{after}}, "tenant-a/deployment/service")
	if !strings.Contains(output.String(), "Pod app — Pod configured limit: 384Mi -> 512Mi") {
		t.Fatal(output.String())
	}
	if doc := podExplanationDocument(after); doc.Containers[0].ConfiguredResources.Request.Bytes != 160<<20 {
		t.Fatal("lost container configuration")
	}
}

func TestReplayQuotesControlCharactersInCaptureCaveats(t *testing.T) {
	bundle := api.IncidentBundle{SchemaVersion: 1, Pods: []api.PodSnapshot{api.LegacyPodSnapshot(resourceCLIPod())}, Caveats: []string{"untrusted\x1b[31m\nstatus"}}
	body, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "capture.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	command := newReplayCommand()
	command.SetOut(&output)
	command.SetArgs([]string{path})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "\x1b") || !strings.Contains(output.String(), `untrusted\x1b[31m\nstatus`) {
		t.Fatal("replay emitted active terminal controls or hid the caveat")
	}
}
