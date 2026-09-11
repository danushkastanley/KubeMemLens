package incident

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/observation"
)

func restrictedFixture(t *testing.T) RestrictedBundle {
	t.Helper()
	at := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
	status := capability.Envelope{Source: capability.KubernetesStatus, APIVersion: "v1", ReceivedAt: at, Scope: capability.PodScope, Freshness: capability.Fresh, Completeness: capability.Complete, Stability: capability.Stable}
	zero := uint64(0)
	set := observation.WorkingSet{Bytes: &zero, Availability: capability.Available, Coverage: observation.Coverage{Reported: 1, Expected: 1, Unit: capability.ContainerScope}, Evidence: capability.Envelope{Source: capability.KubernetesMetrics, APIVersion: "metrics.k8s.io/v1beta1", CapturedAt: at.Add(-time.Minute), ReceivedAt: at, Window: 15 * time.Second, Scope: capability.ContainerScope, Freshness: capability.Fresh, Completeness: capability.Complete, Stability: capability.Beta}}
	pod := observation.Pod{Namespace: "test", Name: "pod", UID: "private-uid", Context: api.PodContext{Labels: map[string]string{"private": "value"}}, StatusEvidence: status, OwnerEvidence: status, OwnerAvailability: capability.Available, Containers: []observation.Container{{Name: "worker", Kind: "application", State: "running", WorkingSet: set}}}
	pod.WorkingSet = observation.SumWorkingSets([]observation.WorkingSet{set}, capability.PodScope)
	batch := observation.Batch{Mode: capability.Restricted, ReceivedAt: at, Completeness: capability.Complete, Sources: []observation.SourceReport{{SourceState: capability.SourceState{Source: capability.KubernetesStatus, APIVersion: "v1", Availability: capability.Available, Freshness: capability.Fresh, Completeness: capability.Complete, Stability: capability.Stable}, Scope: capability.PodScope}}, Pods: []observation.Pod{pod}}
	bundle, err := NewRestricted(batch, "test", "pod", "test", at, false)
	if err != nil {
		t.Fatal(err)
	}
	if batch.Pods[0].UID == "" || len(batch.Pods[0].Context.Labels) == 0 {
		t.Fatal("redaction mutated original frame")
	}
	return bundle
}

func TestRestrictedRoundTripAndStrictValidation(t *testing.T) {
	bundle := restrictedFixture(t)
	path := filepath.Join(t.TempDir(), "capture.json")
	if err := WriteRestricted(io.Discard, path, false, bundle); err != nil {
		t.Fatal(err)
	}
	document, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if document.Restricted.Observations.Pods[0].WorkingSet.Bytes == nil || *document.Restricted.Observations.Pods[0].WorkingSet.Bytes != 0 {
		t.Fatal("measured zero lost")
	}
	for name, mutate := range map[string]func(*RestrictedBundle){
		"mode":             func(b *RestrictedBundle) { b.Observations.Mode = capability.Deep },
		"schema":           func(b *RestrictedBundle) { b.SchemaVersion = 2 },
		"time":             func(b *RestrictedBundle) { b.CapturedAt = time.Time{} },
		"receive time":     func(b *RestrictedBundle) { b.Observations.ReceivedAt = b.CapturedAt.Add(time.Hour) },
		"quantity":         func(b *RestrictedBundle) { v := ^uint64(0); b.Observations.Pods[0].Containers[0].WorkingSet.Bytes = &v },
		"source":           func(b *RestrictedBundle) { b.Observations.Pods[0].WorkingSet.Evidence.Source = capability.Cgroup },
		"api":              func(b *RestrictedBundle) { b.Observations.Pods[0].WorkingSet.Evidence.APIVersion = "v1" },
		"scope":            func(b *RestrictedBundle) { b.Observations.Pods[0].WorkingSet.Evidence.Scope = capability.NodeScope },
		"window":           func(b *RestrictedBundle) { b.Observations.Pods[0].Containers[0].WorkingSet.Evidence.Window = -1 },
		"coverage":         func(b *RestrictedBundle) { b.Observations.Pods[0].WorkingSet.Coverage.Reported = 10 },
		"sum":              func(b *RestrictedBundle) { v := uint64(42); b.Observations.Pods[0].WorkingSet.Bytes = &v },
		"group":            func(b *RestrictedBundle) { b.Observations.Namespaces = nil },
		"cgroup":           func(b *RestrictedBundle) { b.Observations.Pods[0].Cgroup = &observation.Cgroup{} },
		"container cgroup": func(b *RestrictedBundle) { b.Observations.Pods[0].Containers[0].Cgroup = &observation.Cgroup{} },
		"redacted UID":     func(b *RestrictedBundle) { b.Observations.Pods[0].UID = "secret" },
		"controls":         func(b *RestrictedBundle) { b.Observations.Caveats = []string{"bad\x1b[31m"} },
		"long text":        func(b *RestrictedBundle) { b.ToolVersion = strings.Repeat("x", 4097) },
		"duplicate Pod":    func(b *RestrictedBundle) { b.Observations.Pods = append(b.Observations.Pods, b.Observations.Pods[0]) },
	} {
		t.Run(name, func(t *testing.T) {
			copy := restrictedFixture(t)
			mutate(&copy)
			var output bytes.Buffer
			if err := WriteRestricted(&output, "-", false, copy); err == nil || output.Len() != 0 {
				t.Fatalf("invalid capture exported: %v", err)
			}
		})
	}
}

func TestRestrictedReaderRejectsUnknownAndMixedFields(t *testing.T) {
	data, err := json.Marshal(restrictedFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{
		append(append([]byte(nil), data...), []byte(" {}")...),
		bytes.Replace(data, []byte(`"schemaVersion":3`), []byte(`"schemaVersion":3,"histories":[]`), 1),
		bytes.Replace(data, []byte(`"schemaVersion":3`), []byte(`"schemaVersion":2`), 1),
	} {
		path := filepath.Join(t.TempDir(), "bad.json")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Read(path); err == nil {
			t.Fatal("invalid restricted document accepted")
		}
	}
}
