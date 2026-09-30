package incidentsession

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

func markerBundle(t testing.TB, p Principal, at time.Time, coverage changemarkers.Coverage) api.HistoryIncident {
	t.Helper()
	selected := memoryhistory.Selection{Request: memoryhistory.Request{Scope: memoryhistory.Pod, Namespace: p.Namespace, Name: "private-app"}, UID: "pod-uid", ResolvedAt: at,
		Targets: []memoryhistory.Target{{Namespace: p.Namespace, Pod: "private-app", PodUID: "pod-uid", Container: "app", ContainerID: "containerd://private-id", Node: "node-a", NodeUID: "node-uid", PodCreatedAt: at.Add(-time.Minute), StartedAt: at.Add(-time.Minute)}}}
	query := memoryhistory.Query{Source: memoryhistory.Prometheus, Metric: memoryhistory.WorkingSet, Start: at.Add(-time.Minute), End: at, Step: time.Minute}
	history, err := memoryhistory.NewReport(selected, query, at)
	if err != nil {
		t.Fatal(err)
	}
	history.Series[0].Origin = "cadvisor"
	history.Series[0].SampleClock = "prometheus-sample"
	changes, err := changemarkers.Compose(selected, query, at, coverage, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	bundle := api.HistoryIncident{SchemaVersion: 6, CapturedAt: at, ToolVersion: "test", Context: changemarkers.Context{SchemaVersion: 1, History: history, Changes: changes}, Caveats: api.HistoryIncidentCaveats()}
	if err := api.ValidateHistoryIncident(bundle); err != nil {
		t.Fatal(err)
	}
	return bundle
}

func TestMarkersRetainCanonicalHistoryAndVisibleCoverage(t *testing.T) {
	for _, coverage := range []changemarkers.Coverage{changemarkers.Partial, changemarkers.Missing, changemarkers.Denied, changemarkers.Disabled, changemarkers.Unavailable} {
		t.Run(string(coverage), func(t *testing.T) {
			s, clk, _, p := fixture(t, DefaultLimits())
			defer s.Shutdown()
			id := start(t, s, p).ID
			bundle := markerBundle(t, p, clk.Now(), coverage)
			data, err := json.Marshal(bundle)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.RecordMarkers(context.Background(), p, id, data); err != nil {
				t.Fatal(err)
			}
			doc := captureExport(t, s, p, id)
			if len(doc.Captures) != 1 || doc.Captures[0].SchemaVersion != 6 || doc.Entries[1].Kind != Marked {
				t.Fatal("marker payload not retained")
			}
			visible, err := s.Export(context.Background(), p, id, ExportSanitised)
			if err != nil {
				t.Fatal(err)
			}
			public, err := DecodeExport(visible)
			if err != nil {
				t.Fatal(err)
			}
			reasons := map[string]bool{}
			for _, entry := range public.Sanitised.Entries {
				if entry.Kind == Gap {
					reasons[entry.GapReason] = true
				}
			}
			if !reasons["events-"+string(coverage)] || !reasons["history-missing"] {
				t.Fatal("coverage states hidden", reasons)
			}
			if strings.Contains(string(visible), "private-app") || strings.Contains(string(visible), "containerd") || strings.Contains(string(visible), "pod-uid") {
				t.Fatal("marker identity leaked")
			}
		})
	}
}

func TestMarkerValidationAndAtomicGaps(t *testing.T) {
	limits := DefaultLimits()
	limits.Entries = 4
	s, clk, _, p := fixture(t, limits)
	defer s.Shutdown()
	id := start(t, s, p).ID
	bundle := markerBundle(t, p, clk.Now(), changemarkers.Missing)
	data, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordMarkers(context.Background(), p, id, data); !errors.Is(err, ErrCapacity) {
		t.Fatal("partial marker action accepted", err)
	}
	doc := captureExport(t, s, p, id)
	if len(doc.Captures) != 0 || len(doc.Entries) != 1 || !doc.LimitReached {
		t.Fatal("marker gap transaction partially committed")
	}
	other := p
	other.Namespace = "other"
	if _, err := historyCapture(other, data); !errors.Is(err, ErrInvalid) {
		t.Fatal("foreign marker capture accepted")
	}
	bundle.Redacted = true
	data, err = json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := historyCapture(p, data); !errors.Is(err, ErrInvalid) {
		t.Fatal("redacted aliases accepted as live identity")
	}
	if _, err := s.Close(context.Background(), p, id); err != nil {
		t.Fatal("close reserve lost", err)
	}
}

func TestMarkerComparisonRetainsSourceChanges(t *testing.T) {
	s, clk, _, p := fixture(t, DefaultLimits())
	defer s.Shutdown()
	id := start(t, s, p).ID
	first := markerBundle(t, p, clk.Now(), changemarkers.Partial)
	second := markerBundle(t, p, clk.Now().Add(time.Second), changemarkers.Missing)
	for _, bundle := range []api.HistoryIncident{first, second} {
		data, err := json.Marshal(bundle)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.RecordMarkers(context.Background(), p, id, data); err != nil {
			t.Fatal(err)
		}
	}
	doc := captureExport(t, s, p, id)
	clk.Step(2 * time.Second)
	if _, err := s.CompareCaptures(context.Background(), p, id, doc.Captures[0].Digest, doc.Captures[1].Digest); err != nil {
		t.Fatal(err)
	}
	result := captureExport(t, s, p, id)
	// The fixture's target lifetime changed, even though the Pod name and UID did
	// not. The comparison must retain that discontinuity rather than join samples.
	if result.Entries[len(result.Entries)-1].GapReason != "source-changed" {
		t.Fatal("history target lifetime change hidden")
	}
	if result.Entries[len(result.Entries)-2].Kind != Compared {
		t.Fatal("history comparison action lost")
	}
}

func TestRetainedEvidenceRequiresExplicitPrivacyFlag(t *testing.T) {
	p := Principal{"tenant", "namespace-uid", "operator"}
	bundle := markerBundle(t, p, time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC), changemarkers.Partial)
	history, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	pod := captureBytes(t, p, bundle.CapturedAt, "capture")
	for _, data := range [][]byte{history, pod} {
		for _, bad := range []string{strings.Replace(string(data), `"redacted":false,`, "", 1), strings.Replace(string(data), `"redacted":false`, `"redacted":null`, 1)} {
			if _, err := decodeRetainedCapture(p, []byte(bad)); !errors.Is(err, ErrInvalid) {
				t.Fatal("missing privacy declaration accepted")
			}
		}
	}
}

func FuzzRetainedEvidence(f *testing.F) {
	p := Principal{"tenant", "namespace-uid", "operator"}
	bundle := markerBundle(f, p, time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC), changemarkers.Partial)
	seed, err := json.Marshal(bundle)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed)
	f.Add([]byte(`{"schemaVersion":6,"redacted":false}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > MaxCaptureBytes+1 {
			return
		}
		evidence, err := decodeRetainedCapture(p, data)
		if err != nil {
			return
		}
		if len(evidence.Data) == 0 || len(evidence.Data) > MaxCaptureBytes || evidence.NamespaceUID != p.NamespaceUID || evidence.ObservedAt.IsZero() {
			t.Fatal("invalid retained evidence")
		}
		again, err := decodeRetainedCapture(p, evidence.Data)
		if err != nil || again.reference() != evidence.reference() {
			t.Fatal("retained evidence lost provenance on round trip")
		}
	})
}
