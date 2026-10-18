package incidentsession

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

func captureBytes(t *testing.T, p Principal, at time.Time, caveat string) []byte {
	t.Helper()
	bundle := api.IncidentBundle{SchemaVersion: 1, CapturedAt: at, ToolVersion: "test", Partial: true,
		Pods: []api.PodSnapshot{{Namespace: p.Namespace, PodName: "app", PodUID: "pod-uid", CapturedAt: at}}, Caveats: []string{caveat}}
	data, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func captureExport(t *testing.T, s *Store, p Principal, id string) AuthorisedExport {
	t.Helper()
	data, err := s.Export(context.Background(), p, id, ExportAuthorised)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeExport(data)
	if err != nil || decoded.Authorised == nil {
		t.Fatal("capture export invalid", err)
	}
	return *decoded.Authorised
}

func TestCaptureRetentionExportAndCopyIsolation(t *testing.T) {
	s, clk, _, p := fixture(t, DefaultLimits())
	defer s.Shutdown()
	id := start(t, s, p).ID
	data := captureBytes(t, p, clk.Now(), "private capture context")
	original := bytes.Clone(data)
	summary, err := s.RecordCapture(context.Background(), p, id, data)
	if err != nil || summary.Entries != 2 {
		t.Fatal("capture not recorded", err)
	}
	data[0] = 'x'
	doc := captureExport(t, s, p, id)
	if len(doc.Captures) != 1 || !bytes.Equal(doc.Captures[0].Data, original) || doc.Entries[1].References[0] != doc.Captures[0].reference() {
		t.Fatal("capture bytes or provenance lost")
	}
	read, err := s.CaptureData(context.Background(), p, id, doc.Captures[0].Digest)
	if err != nil || !bytes.Equal(read, original) {
		t.Fatal("capture unavailable", err)
	}
	read[0] = 'x'
	doc.Captures[0].Data[0] = 'x'
	if got := captureExport(t, s, p, id); !bytes.Equal(got.Captures[0].Data, original) {
		t.Fatal("returned bytes alias stored evidence")
	}
	if _, err := s.RecordCapture(context.Background(), p, id, original); err != nil {
		t.Fatal(err)
	}
	if got := captureExport(t, s, p, id); len(got.Captures) != 1 || len(got.Entries) != 3 {
		t.Fatal("repeated capture lost action or duplicated payload")
	}
	sanitised, err := s.Export(context.Background(), p, id, ExportSanitised)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sanitised), "captures") || strings.Contains(string(sanitised), "private capture") || strings.Contains(string(sanitised), "pod-uid") {
		t.Fatal("private capture leaked")
	}
	if _, err := DecodeExport(sanitised); err != nil {
		t.Fatal(err)
	}
}

func TestCaptureAuthorisationAndLifetime(t *testing.T) {
	s, clk, auth, p := fixture(t, DefaultLimits())
	defer s.Shutdown()
	id := start(t, s, p).ID
	data := captureBytes(t, p, clk.Now(), "bounded")
	auth.deny = true
	if _, err := s.RecordCapture(context.Background(), p, id, data); !errors.Is(err, ErrDenied) {
		t.Fatal("capture bypassed permission")
	}
	auth.deny = false
	if _, err := s.RecordCapture(context.Background(), p, id, data); err != nil {
		t.Fatal(err)
	}
	doc := captureExport(t, s, p, id)
	digest := doc.Captures[0].Digest
	other := p
	other.Actor = "another-actor"
	if _, err := s.CaptureData(context.Background(), other, id, digest); !errors.Is(err, ErrNotFound) {
		t.Fatal("capture leaked to another owner")
	}
	if _, err := s.Close(context.Background(), p, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordCapture(context.Background(), p, id, data); !errors.Is(err, ErrClosed) {
		t.Fatal("closed session retained new capture")
	}
	clk.Step(DefaultLimits().Retention)
	if _, err := s.CaptureData(context.Background(), p, id, digest); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired capture retained")
	}
}

func TestCaptureCapacityIsAtomicAndPreservesClose(t *testing.T) {
	limits := DefaultLimits()
	limits.SessionBytes = 16 << 10
	s, clk, _, p := fixture(t, limits)
	defer s.Shutdown()
	id := start(t, s, p).ID
	data := captureBytes(t, p, clk.Now(), strings.Repeat("x", 12000))
	if _, err := s.RecordCapture(context.Background(), p, id, data); !errors.Is(err, ErrCapacity) {
		t.Fatal("encoded payload escaped shared session budget", err)
	}
	doc := captureExport(t, s, p, id)
	if len(doc.Captures) != 0 || len(doc.Entries) != 1 || !doc.LimitReached {
		t.Fatal("failed capture partially mutated state")
	}
	if _, err := s.Close(context.Background(), p, id); err != nil {
		t.Fatal("capture consumed close reserve", err)
	}
}

func TestCaptureValidatesTenantAndNestedIdentity(t *testing.T) {
	p := Principal{"tenant", "namespace-uid", "operator"}
	data := captureBytes(t, p, time.Now().UTC(), "capture")
	for _, mutate := range []func(*api.IncidentBundle){
		func(b *api.IncidentBundle) { b.Pods[0].Namespace = "other" },
		func(b *api.IncidentBundle) { b.Pods[0].PodUID = "" },
		func(b *api.IncidentBundle) { b.Nodes = []api.NodeSnapshotStatus{{NodeName: "private-node"}} },
		func(b *api.IncidentBundle) {
			b.Pods[0].Containers = []api.ContainerSnapshot{{Namespace: "other", PodName: "app", PodUID: "pod-uid"}}
		},
		func(b *api.IncidentBundle) {
			b.Histories = []api.PodHistory{{Namespace: p.Namespace, PodName: "app", PodUID: "old-uid"}}
		},
		func(b *api.IncidentBundle) { b.Redacted = true },
		func(b *api.IncidentBundle) { b.SchemaVersion = 99 },
	} {
		var bundle api.IncidentBundle
		if err := json.Unmarshal(data, &bundle); err != nil {
			t.Fatal(err)
		}
		mutate(&bundle)
		bad, err := json.Marshal(bundle)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := podCapture(p, bad); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid capture accepted")
		}
	}
	if _, err := podCapture(p, bytes.Repeat([]byte(" "), MaxCaptureBytes+1)); err == nil {
		t.Fatal("oversized capture accepted")
	}
	if boundedCaptureData([]byte(`{"pods":[`+strings.Repeat(`null,`, 128)+`null]}`)) == nil {
		t.Fatal("array allocated before structural bound")
	}
}

func TestCaptureExportRejectsTamperedMissingAndOrphanedData(t *testing.T) {
	s, clk, _, p := fixture(t, DefaultLimits())
	defer s.Shutdown()
	id := start(t, s, p).ID
	if _, err := s.RecordCapture(context.Background(), p, id, captureBytes(t, p, clk.Now(), "capture")); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*AuthorisedExport){
		func(d *AuthorisedExport) { d.Captures[0].Data[0] = 'x' },
		func(d *AuthorisedExport) { d.Captures[0].Digest = strings.Repeat("a", 64) },
		func(d *AuthorisedExport) { d.Captures[0].NamespaceUID = "other" },
		func(d *AuthorisedExport) { d.Captures[0].ObservedAt = d.Captures[0].ObservedAt.Add(time.Second) },
		func(d *AuthorisedExport) { d.Captures[0].Data = nil },
		func(d *AuthorisedExport) { d.Captures = append(d.Captures, d.Captures[0]) },
		func(d *AuthorisedExport) { d.Entries = d.Entries[:1] },
	} {
		doc := captureExport(t, s, p, id)
		mutate(&doc)
		data, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeExport(data); !errors.Is(err, ErrInvalid) {
			t.Fatal("tampered capture accepted")
		}
	}
	if err := s.Delete(context.Background(), p, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CaptureData(context.Background(), p, id, strings.Repeat("a", 64)); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted evidence returned")
	}
}
