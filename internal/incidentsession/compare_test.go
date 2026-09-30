package incidentsession

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

func retainedPair(t *testing.T, s *Store, p Principal, id string, at time.Time, change func(*api.IncidentBundle)) (string, string) {
	t.Helper()
	first := captureBytes(t, p, at, "before")
	var next api.IncidentBundle
	if json.Unmarshal(first, &next) != nil {
		t.Fatal("fixture")
	}
	next.CapturedAt = at.Add(time.Second)
	next.Pods[0].CapturedAt = next.CapturedAt
	next.Caveats = []string{"after"}
	change(&next)
	second, err := json.Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{first, second} {
		if _, err := s.RecordCapture(context.Background(), p, id, data); err != nil {
			t.Fatal(err)
		}
	}
	doc := captureExport(t, s, p, id)
	return doc.Captures[0].Digest, doc.Captures[1].Digest
}

func TestCompareRetainsOrderAndDiscontinuities(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*api.IncidentBundle)
		gap    string
	}{
		{"same instance", func(*api.IncidentBundle) {}, ""},
		{"schema upgrade", func(b *api.IncidentBundle) { b.SchemaVersion = 2 }, ""},
		{"replacement", func(b *api.IncidentBundle) { b.Pods[0].PodUID = "replacement" }, "source-changed"},
		{"different Pod", func(b *api.IncidentBundle) { b.Pods[0].PodName = "other" }, "source-changed"},
		{"clock regression", func(b *api.IncidentBundle) {
			b.CapturedAt = b.CapturedAt.Add(-2 * time.Second)
			b.Pods[0].CapturedAt = b.CapturedAt
		}, "clock-uncertain"},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, clk, _, p := fixture(t, DefaultLimits())
			defer s.Shutdown()
			id := start(t, s, p).ID
			before, after := retainedPair(t, s, p, id, clk.Now(), test.change)
			clk.Step(2 * time.Second)
			if _, err := s.CompareCaptures(context.Background(), p, id, before, after); err != nil {
				t.Fatal(err)
			}
			doc := captureExport(t, s, p, id)
			action := doc.Entries[3]
			if action.Kind != Compared || action.References[0].Digest != before || action.References[1].Digest != after {
				t.Fatal("ordered comparison not retained")
			}
			expected := 4
			if test.gap != "" {
				expected = 5
				gap := doc.Entries[4]
				if gap.Kind != Gap || gap.GapReason != test.gap {
					t.Fatal("discontinuity hidden")
				}
				if test.gap == "clock-uncertain" && !gap.ClockUncertain {
					t.Fatal("clock uncertainty hidden")
				}
			}
			if len(doc.Entries) != expected {
				t.Fatal("unexpected timeline mutation")
			}
			data, err := s.Export(context.Background(), p, id, ExportSanitised)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := DecodeExport(data)
			if err != nil || decoded.Sanitised == nil || len(decoded.Sanitised.Entries) != expected {
				t.Fatal("public chronology invalid", err)
			}
		})
	}
}

func TestCompareGapsAndActionAreAtomic(t *testing.T) {
	limits := DefaultLimits()
	limits.Entries = 5
	s, clk, _, p := fixture(t, limits)
	defer s.Shutdown()
	id := start(t, s, p).ID
	before, after := retainedPair(t, s, p, id, clk.Now(), func(b *api.IncidentBundle) { b.Pods[0].PodUID = "replacement" })
	if _, err := s.CompareCaptures(context.Background(), p, id, before, after); !errors.Is(err, ErrCapacity) {
		t.Fatal("comparison bypassed gap/close reserve", err)
	}
	doc := captureExport(t, s, p, id)
	if len(doc.Entries) != 3 || !doc.LimitReached {
		t.Fatal("failed comparison partially recorded")
	}
	if _, err := s.Close(context.Background(), p, id); err != nil {
		t.Fatal("close reserve lost", err)
	}
}

func TestCompareRequiresRetainedOwnedEvidence(t *testing.T) {
	s, clk, auth, p := fixture(t, DefaultLimits())
	defer s.Shutdown()
	id := start(t, s, p).ID
	before, after := retainedPair(t, s, p, id, clk.Now(), func(*api.IncidentBundle) {})
	other := p
	other.Actor = "other"
	if _, err := s.CompareCaptures(context.Background(), other, id, before, after); !errors.Is(err, ErrNotFound) {
		t.Fatal("owner bypass")
	}
	if _, err := s.CompareCaptures(context.Background(), p, id, strings.Repeat("a", 64), after); !errors.Is(err, ErrNotFound) {
		t.Fatal("unretained reference accepted")
	}
	if _, err := s.CompareCaptures(context.Background(), p, id, "invalid", after); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid digest accepted")
	}
	auth.deny = true
	if _, err := s.CompareCaptures(context.Background(), p, id, before, after); !errors.Is(err, ErrDenied) {
		t.Fatal("revocation bypass")
	}
	auth.deny = false
	if _, err := s.Close(context.Background(), p, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompareCaptures(context.Background(), p, id, before, after); !errors.Is(err, ErrClosed) {
		t.Fatal("closed session mutated")
	}
}
