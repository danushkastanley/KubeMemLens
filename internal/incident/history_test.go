package incident

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

func historyCaptureFixture(t testing.TB, at time.Time, uid string) changemarkers.Context {
	t.Helper()
	s := memoryhistory.Selection{Request: memoryhistory.Request{Scope: memoryhistory.Pod, Namespace: "tenant-secret", Name: "private-app"}, UID: uid, ResolvedAt: at, Targets: []memoryhistory.Target{{Namespace: "tenant-secret", Pod: "private-app", PodUID: uid, Container: "private-container", ContainerID: "containerd://private-runtime", Node: "private-node", NodeUID: "private-node-uid", PodCreatedAt: at.Add(-50 * time.Second), StartedAt: at.Add(-40 * time.Second)}}}
	q := memoryhistory.Query{Source: memoryhistory.Prometheus, Metric: memoryhistory.WorkingSet, Start: at.Add(-time.Minute), End: at, Step: time.Minute}
	h, err := memoryhistory.NewReport(s, q, at)
	if err != nil {
		t.Fatal(err)
	}
	h.Series[0].Origin = "cadvisor"
	h.Series[0].SampleClock = "prometheus-sample"
	pod := changemarkers.Object{APIVersion: "v1", Kind: "Pod", Namespace: "tenant-secret", Name: "private-app", UID: uid}
	owner := changemarkers.Object{APIVersion: "apps/v1", Kind: "ReplicaSet", Namespace: "tenant-secret", Name: "private-owner", UID: "private-owner-uid"}
	var markers []changemarkers.Marker
	for i := 0; i < 32; i++ {
		markers = append(markers, changemarkers.Marker{Kind: changemarkers.Started, Clock: changemarkers.KubernetesEvent, At: at.Add(-20 * time.Second), Until: at.Add(-20 * time.Second), Subject: pod, Owners: []changemarkers.Object{owner}, SourceUID: fmt.Sprintf("private-event-%02d", i), Count: 1, Uncertain: true})
	}
	changes, err := changemarkers.Compose(s, q, at, changemarkers.Partial, markers, false)
	if err != nil {
		t.Fatal(err)
	}
	return changemarkers.Context{SchemaVersion: 1, History: h, Changes: changes}
}

func TestHistoryCaptureRedactsIdentityAndRetainsProvenance(t *testing.T) {
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	c := historyCaptureFixture(t, at, "private-pod-uid")
	b, err := NewHistory(c, "test", at, false)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(b)
	for _, raw := range []string{"private-", "tenant-secret", "containerd://"} {
		if bytes.Contains(body, []byte(raw)) {
			t.Fatalf("raw identity escaped: %s", raw)
		}
	}
	if c.History.Selection.Request.Name != "private-app" || c.Changes.Markers[0].Owners[0].UID != "private-owner-uid" {
		t.Fatal("capture mutated live evidence")
	}
	if !b.Redacted || len(b.Context.Changes.Markers) != 32 || b.Context.Changes.Markers[0].Clock != changemarkers.KubernetesEvent || !b.Context.Changes.Markers[0].At.Equal(at.Add(-20*time.Second)) {
		t.Fatal("redaction lost source time or evidence")
	}
	path := filepath.Join(t.TempDir(), "history.json")
	if err := WriteHistory(io.Discard, path, false, b); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("capture file was not private", err)
	}
	doc, err := Read(path)
	if err != nil || doc.History == nil || doc.Deep != nil || !doc.History.Redacted {
		t.Fatal("schema-6 roundtrip", err)
	}
	var exists ExistsError
	if err := WriteHistory(io.Discard, path, false, b); !errors.As(err, &exists) {
		t.Fatal("capture overwrote existing file", err)
	}
}

func TestLocalPodChargeCaptureKeepsContainerlessSeries(t *testing.T) {
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	c := historyCaptureFixture(t, at, "private-pod-uid")
	c.History.Query.Source = memoryhistory.Local
	c.History.Query.Metric = memoryhistory.Charge
	s := &c.History.Series[0]
	s.Source = memoryhistory.Local
	s.Metric = memoryhistory.Charge
	s.Origin = "cgroup-v2"
	s.SampleClock = "collector-capture"
	s.Target.Container = ""
	s.Target.ContainerID = ""
	s.Target.StartedAt = s.Target.PodCreatedAt
	b, err := NewHistory(c, "test", at, false)
	if err != nil || b.Context.History.Series[0].Target.Container != "" || b.Context.History.Validate() != nil {
		t.Fatal("local charge semantics changed", err)
	}
}

func TestHistoryComparisonDoesNotJoinAliasedOrReplacedIdentity(t *testing.T) {
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	for _, redacted := range []bool{false, true} {
		before, err := NewHistory(historyCaptureFixture(t, at, "before-pod-uid"), "test", at, !redacted)
		if err != nil {
			t.Fatal(err)
		}
		later := at.Add(time.Minute)
		after, err := NewHistory(historyCaptureFixture(t, later, "after-pod-uid"), "test", later, !redacted)
		if err != nil {
			t.Fatal(err)
		}
		result, err := CompareHistory(before, after)
		if err != nil {
			t.Fatal(err)
		}
		if redacted {
			if result.Continuity != "identity-unavailable" || len(result.Replacements) != 0 {
				t.Fatal("aliases asserted cross-capture continuity")
			}
			continue
		}
		if result.Continuity != "different-instances" || len(result.Replacements) != 1 {
			t.Fatal("replacement evidence lost", result)
		}
		marker := result.Replacements[0]
		if marker.PreviousUID != "before-pod-uid" || marker.Subject.UID != "after-pod-uid" || !marker.At.Equal(at) || !marker.Until.Equal(later) || len(marker.Owners) != 1 || !marker.Uncertain {
			t.Fatal("replacement interval/provenance changed", marker)
		}
		if _, err := CompareHistory(after, before); err == nil {
			t.Fatal("reverse chronology accepted")
		}
	}
}

func TestHistoryDecoderRejectsMisleadingRedactionAndLostCaveats(t *testing.T) {
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	b, err := NewHistory(historyCaptureFixture(t, at, "private-pod-uid"), "test", at, true)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(b)
	for _, test := range []struct {
		name string
		body []byte
	}{
		{"false redaction", bytes.Replace(raw, []byte(`"redacted":false`), []byte(`"redacted":true`), 1)},
		{"null redaction", bytes.Replace(raw, []byte(`"redacted":false`), []byte(`"redacted":null`), 1)},
		{"duplicate metadata", bytes.Replace(raw, []byte(`"redacted":false`), []byte(`"redacted":false,"Redacted":true`), 1)},
		{"lost caveat", bytes.Replace(raw, []byte(historyCaveats[0]), []byte("complete change log"), 1)},
		{"unbounded body", []byte(strings.Repeat(" ", MaxHistoryBytes+1))},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := decodeHistory(test.body); err == nil {
				t.Fatal("misleading capture accepted")
			}
		})
	}
}

func maximumHistoryCaptureContext(t *testing.T) (changemarkers.Context, time.Time) {
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	c := historyCaptureFixture(t, at, "private-pod-uid")
	s := c.History.Selection
	s.Request = memoryhistory.Request{Scope: memoryhistory.Workload, Namespace: "tenant-secret", Name: "private-workload", WorkloadKind: "Deployment"}
	s.UID = "private-workload-uid"
	s.Targets = nil
	base := c.History.Selection.Targets[0]
	for i := 0; i < memoryhistory.MaxTargets; i++ {
		target := base
		target.Pod = fmt.Sprintf("private-app-%02d", i)
		target.PodUID = fmt.Sprintf("private-pod-%02d", i)
		target.ContainerID = fmt.Sprintf("containerd://private-runtime-%02d", i)
		target.StartedAt = at.Add(-24 * time.Hour)
		target.PodCreatedAt = target.StartedAt.Add(-time.Minute)
		s.Targets = append(s.Targets, target)
	}
	q := c.History.Query
	q.Start = at.Add(-240 * time.Minute)
	history, err := memoryhistory.NewReport(s, q, at)
	if err != nil {
		t.Fatal(err)
	}
	bytesValue := uint64(1 << 63)
	for i := range history.Series {
		series := &history.Series[i]
		series.Origin = "cadvisor"
		series.SampleClock = "prometheus-sample"
		for j := range series.Points {
			point := &series.Points[j]
			point.SampledAt = point.At
			point.Bytes = &bytesValue
			point.State = memoryhistory.Fresh
		}
	}
	memoryhistory.Summarise(&history, false)
	var markers []changemarkers.Marker
	for i := 0; i < changemarkers.MaxMarkers; i++ {
		target := s.Targets[i%len(s.Targets)]
		when := at.Add(-time.Second)
		markers = append(markers, changemarkers.Marker{Kind: changemarkers.Started, Clock: changemarkers.KubernetesEvent, At: when, Until: when, SourceUID: fmt.Sprintf("private-event-%02d", i), Subject: changemarkers.Object{APIVersion: "v1", Kind: "Pod", Namespace: target.Namespace, Name: target.Pod, UID: target.PodUID}, Owners: []changemarkers.Object{{APIVersion: "apps/v1", Kind: "Deployment", Namespace: s.Request.Namespace, Name: s.Request.Name, UID: s.UID}}, Count: 1, Uncertain: true})
	}
	changes, err := changemarkers.Compose(s, q, at, changemarkers.Partial, markers, false)
	if err != nil {
		t.Fatal(err)
	}
	c = changemarkers.Context{SchemaVersion: 1, History: history, Changes: changes}
	return c, at
}

func TestMaximumHistoryCaptureRemainsReadableAfterFormattedWrite(t *testing.T) {
	c, at := maximumHistoryCaptureContext(t)
	bundle, err := NewHistory(c, "test", at, false)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "maximum.json")
	if err := WriteHistory(io.Discard, path, false, bundle); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	t.Logf("formatted capture bytes=%d", info.Size())
	document, err := Read(path)
	if err != nil {
		t.Fatal("own formatted maximum capture cannot be read", err)
	}
	if len(document.History.Context.History.Series) != memoryhistory.MaxTargets || len(document.History.Context.History.Series[0].Points) != memoryhistory.MaxPoints || len(document.History.Context.Changes.Markers) != changemarkers.MaxMarkers {
		t.Fatal("maximum evidence was truncated during capture roundtrip")
	}
}

func TestSensitiveCapturePreservesBoundedLargeOwnerProvenance(t *testing.T) {
	c, at := maximumHistoryCaptureContext(t)
	c.History.Selection.Request.Name = strings.Repeat("d", 50) + "." + strings.Repeat("d", 50) + "." + strings.Repeat("d", 50)
	c.History.Selection.UID = strings.Repeat("d", 200)
	c.Changes.Request = c.History.Selection.Request
	c.Changes.UID = c.History.Selection.UID
	root := changemarkers.Object{APIVersion: "apps/v1", Kind: "Deployment", Namespace: c.Changes.Request.Namespace, Name: c.Changes.Request.Name, UID: c.Changes.UID}
	for i := range c.Changes.Markers {
		m := &c.Changes.Markers[i]
		m.SourceUID = fmt.Sprintf("event-%03d-", i) + strings.Repeat("e", 120)
		m.Owners = []changemarkers.Object{
			{APIVersion: "apps/v1", Kind: "ReplicaSet", Namespace: root.Namespace, Name: strings.Repeat("r", 50) + "." + strings.Repeat("r", 50), UID: strings.Repeat("r", 200)},
			root,
			{APIVersion: "apps/v1", Kind: "StatefulSet", Namespace: root.Namespace, Name: strings.Repeat("s", 50) + "." + strings.Repeat("s", 50), UID: strings.Repeat("s", 200)},
		}
	}
	c.Changes.Markers = append([]changemarkers.Marker(nil), c.Changes.Markers...)
	changes, err := changemarkers.Compose(c.History.Selection, c.History.Query, at, changemarkers.Partial, c.Changes.Markers, false)
	if err != nil {
		t.Fatal(err)
	}
	c.Changes = changes
	compact, _ := json.Marshal(changes)
	t.Logf("compact marker bytes=%d", len(compact))
	b, err := NewHistory(c, "test", at, true)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "sensitive.json")
	if err := WriteHistory(io.Discard, path, false, b); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path); err != nil {
		t.Fatal("formatted provenance rejected despite valid compact bounds", err)
	}
}
