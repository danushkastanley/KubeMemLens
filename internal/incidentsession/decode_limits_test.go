package incidentsession

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMaximumTimelineDecodesWithoutDroppingBounds(t *testing.T) {
	s, clk, _, p := fixture(t, DefaultLimits())
	defer s.Shutdown()
	id := start(t, s, p).ID
	ctx := context.Background()
	refs := []CaptureReference{{p.NamespaceUID, strings.Repeat("a", 64), 6, clk.Now()}, {p.NamespaceUID, strings.Repeat("b", 64), 6, clk.Now()}}
	for range MaxEntries - 2 {
		if _, err := s.Append(ctx, p, id, Input{Kind: Compared, Source: "collector", ObservedAt: clk.Now(), References: refs}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Append(ctx, p, id, Input{Kind: Annotated, Source: "operator", Note: "over limit"}); !errors.Is(err, ErrCapacity) {
		t.Fatal("limit bypassed")
	}
	if _, err := s.Close(ctx, p, id); err != nil {
		t.Fatal(err)
	}
	for _, op := range []Operation{ExportAuthorised, ExportSanitised} {
		data, err := s.Export(ctx, p, id, op)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeExport(data); err != nil {
			t.Fatal("maximum bounded export rejected", err)
		}
	}
}

func TestStructuralBoundsRejectBeforeTypedTimelineAllocation(t *testing.T) {
	data := exportFixture(t, ExportAuthorised)
	var doc AuthorisedExport
	if json.Unmarshal(data, &doc) != nil {
		t.Fatal("fixture")
	}
	for len(doc.Entries) <= MaxEntries {
		doc.Entries = append(doc.Entries, doc.Entries[0])
	}
	bad, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if boundedSyntax(bad) == nil {
		t.Fatal("entry count was only checked after typed allocation")
	}
	if json.Unmarshal(data, &doc) != nil {
		t.Fatal("fixture")
	}
	doc.Entries[2].References = append(doc.Entries[2].References, doc.Entries[2].References[0], doc.Entries[2].References[0])
	bad, err = json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if boundedSyntax(bad) == nil {
		t.Fatal("reference count was only checked after typed allocation")
	}
}

func TestConflictingCaptureReceiptCannotEnterStore(t *testing.T) {
	s, clk, _, p := fixture(t, DefaultLimits())
	defer s.Shutdown()
	id := start(t, s, p).ID
	ctx := context.Background()
	ref := CaptureReference{p.NamespaceUID, strings.Repeat("a", 64), 6, clk.Now()}
	in := Input{Kind: Captured, Source: "collector", ObservedAt: clk.Now(), References: []CaptureReference{ref}}
	if _, err := s.Append(ctx, p, id, in); err != nil {
		t.Fatal(err)
	}
	in.References[0].ObservedAt = in.References[0].ObservedAt.Add(-time.Minute)
	if _, err := s.Append(ctx, p, id, in); !errors.Is(err, ErrInvalid) {
		t.Fatal("conflicting digest metadata accepted")
	}
	data, err := s.Export(ctx, p, id, ExportAuthorised)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeExport(data); err != nil {
		t.Fatal("rejected receipt damaged stored export")
	}
}

func TestExplicitClockGapCannotLookCertain(t *testing.T) {
	s, _, _, p := fixture(t, DefaultLimits())
	defer s.Shutdown()
	id := start(t, s, p).ID
	ctx := context.Background()
	if _, err := s.Append(ctx, p, id, Input{Kind: Gap, Source: "collector", GapReason: "clock-uncertain"}); err != nil {
		t.Fatal(err)
	}
	data, err := s.Export(ctx, p, id, ExportAuthorised)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := DecodeExport(data)
	if err != nil || !doc.Authorised.Entries[1].ClockUncertain {
		t.Fatal("explicit clock uncertainty lost")
	}
	doc.Authorised.Entries[1].ClockUncertain = false
	bad, err := json.Marshal(doc.Authorised)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeExport(bad); !errors.Is(err, ErrInvalid) {
		t.Fatal("false certainty accepted")
	}
}

func FuzzDecodeExport(f *testing.F) {
	for _, name := range []string{"authorised-v1", "sanitised-v1", "unsupported-v0", "unknown-future-v2"} {
		data, err := os.ReadFile("testdata/" + name + ".json")
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		doc, err := DecodeExport(data)
		if err != nil {
			if !errors.Is(err, ErrInvalid) {
				t.Fatal("unbounded parser detail exposed")
			}
			return
		}
		if (doc.Authorised == nil) == (doc.Sanitised == nil) {
			t.Fatal("ambiguous disclosure mode")
		}
		var value any = doc.Authorised
		if doc.Sanitised != nil {
			value = doc.Sanitised
		}
		roundtrip, err := json.Marshal(value)
		if err != nil {
			t.Fatal("accepted document cannot be encoded")
		}
		if _, err := DecodeExport(roundtrip); err != nil {
			t.Fatal("accepted document failed round trip")
		}
	})
}
