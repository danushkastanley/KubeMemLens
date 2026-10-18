package incidentsession

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestExportsProjectAllowListAndKeepPrivateProvenance(t *testing.T) {
	s, clk, _, p := fixture(t, DefaultLimits())
	defer s.Shutdown()
	summary := start(t, s, p)
	id := summary.ID
	ctx := context.Background()
	note := "private-token /private/path"
	ref := CaptureReference{p.NamespaceUID, strings.Repeat("a", 64), 6, clk.Now()}
	inputs := []Input{
		{Kind: Annotated, Source: "operator", Note: note},
		{Kind: Captured, Source: "collector", ObservedAt: clk.Now(), References: []CaptureReference{ref}},
		{Kind: Compared, Source: "collector", ObservedAt: clk.Now(), References: []CaptureReference{ref, ref}},
		{Kind: Gap, Source: "prometheus", GapReason: "source-unavailable"},
	}
	for _, in := range inputs {
		if _, err := s.Append(ctx, p, id, in); err != nil {
			t.Fatal(err)
		}
	}
	inputs[1].References[0].Digest = strings.Repeat("b", 64)
	full, err := s.Export(ctx, p, id, ExportAuthorised)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{p.NamespaceUID, p.Actor, note, ref.Digest} {
		if !bytes.Contains(full, []byte(private)) {
			t.Fatal("authorised provenance missing")
		}
	}
	public, err := s.Export(ctx, p, id, ExportSanitised)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{p.Namespace, p.NamespaceUID, p.Actor, note, ref.Digest, id, "namespaceUID", "digest", "note"} {
		if bytes.Contains(public, []byte(private)) {
			t.Fatalf("sanitised export retained private field %q", private)
		}
	}
	var doc SanitisedExport
	if json.Unmarshal(public, &doc) != nil || !doc.Redacted || len(doc.Entries) != 5 {
		t.Fatal("invalid public timeline")
	}
	if doc.Entries[2].References[0].Alias != doc.Entries[3].References[1].Alias || doc.Entries[4].GapReason != "source-unavailable" || doc.Entries[4].ObservedAt != nil {
		t.Fatal("reference continuity or evidence gap lost")
	}
	repeated, err := s.Export(ctx, p, id, ExportSanitised)
	if err != nil || !bytes.Equal(public, repeated) {
		t.Fatal("export ordering is nondeterministic")
	}
	for _, value := range []any{p, inputs[0], ref, s, summary} {
		formatted := fmt.Sprintf("%+v", value)
		for _, private := range []string{p.NamespaceUID, p.Actor, note, ref.Digest, id} {
			if strings.Contains(formatted, private) {
				t.Fatal("formatted private data exposed")
			}
		}
	}
}

func TestClockGapsRemainVisibleWithoutReordering(t *testing.T) {
	s, clk, _, p := fixture(t, DefaultLimits())
	defer s.Shutdown()
	id := start(t, s, p).ID
	ctx := context.Background()
	clk.Step(2 * time.Second)
	ref := CaptureReference{p.NamespaceUID, strings.Repeat("a", 64), 6, clk.Now()}
	if _, err := s.Append(ctx, p, id, Input{Kind: Captured, Source: "collector", ObservedAt: clk.Now(), References: []CaptureReference{ref}}); err != nil {
		t.Fatal(err)
	}
	clk.Step(-time.Second)
	if _, err := s.Append(ctx, p, id, Input{Kind: Annotated, Source: "operator", Note: "clock moved"}); err != nil {
		t.Fatal(err)
	}
	clk.Step(3 * time.Second)
	if _, err := s.Append(ctx, p, id, Input{Kind: Captured, Source: "collector", ObservedAt: ref.ObservedAt.Add(-time.Second), References: []CaptureReference{ref}}); err != nil {
		t.Fatal(err)
	}
	data, err := s.Export(ctx, p, id, ExportAuthorised)
	if err != nil {
		t.Fatal(err)
	}
	var doc AuthorisedExport
	if json.Unmarshal(data, &doc) != nil {
		t.Fatal("decode")
	}
	if !doc.Entries[2].ClockUncertain || !doc.Entries[3].ClockUncertain {
		t.Fatal("clock or source regression hidden")
	}
	for i, e := range doc.Entries {
		if e.Sequence != uint64(i+1) {
			t.Fatal("recording order changed")
		}
	}
}

func TestEncodedByteBoundLeavesCloseCapacity(t *testing.T) {
	limits := DefaultLimits()
	limits.SessionBytes = 16 << 10
	s, _, _, p := fixture(t, limits)
	defer s.Shutdown()
	id := start(t, s, p).ID
	ctx := context.Background()
	for i := 0; i < limits.Entries; i++ {
		_, err := s.Append(ctx, p, id, Input{Kind: Annotated, Source: "operator", Note: strings.Repeat("x", 512)})
		if errors.Is(err, ErrCapacity) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if i == limits.Entries-1 {
			t.Fatal("byte ceiling did not bind")
		}
	}
	if _, err := s.Close(ctx, p, id); err != nil {
		t.Fatal("reserved close failed", err)
	}
	data, err := s.Export(ctx, p, id, ExportAuthorised)
	if err != nil || len(data) > limits.SessionBytes {
		t.Fatal("encoded limit exceeded")
	}
	var private AuthorisedExport
	if json.Unmarshal(data, &private) != nil || !private.LimitReached {
		t.Fatal("full export hid rejected evidence")
	}
	data, err = s.Export(ctx, p, id, ExportSanitised)
	var public SanitisedExport
	if err != nil || json.Unmarshal(data, &public) != nil || !public.LimitReached {
		t.Fatal("sanitised export hid rejected evidence")
	}
}

func TestInvalidInputsCannotChangeTimeline(t *testing.T) {
	s, clk, _, p := fixture(t, DefaultLimits())
	defer s.Shutdown()
	id := start(t, s, p).ID
	ctx := context.Background()
	for _, in := range []Input{
		{Kind: Opened, Source: "operator"}, {Kind: Closed, Source: "operator"},
		{Kind: Kind("trace-reference"), Source: "collector"},
		{Kind: Annotated, Source: "operator", Note: "escape\x1b"},
		{Kind: Annotated, Source: "operator", Note: "bidi\u202e"},
		{Kind: Annotated, Source: "operator", Note: strings.Repeat("x", 513)},
		{Kind: Gap, Source: "collector", GapReason: "private unbounded failure"},
		{Kind: Captured, Source: "collector", ObservedAt: clk.Now(), References: []CaptureReference{{"other-uid", strings.Repeat("a", 64), 6, clk.Now()}}},
	} {
		if _, err := s.Append(ctx, p, id, in); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid Entry accepted")
		}
	}
	data, err := s.Export(ctx, p, id, ExportAuthorised)
	if err != nil {
		t.Fatal(err)
	}
	var doc AuthorisedExport
	if json.Unmarshal(data, &doc) != nil || len(doc.Entries) != 1 {
		t.Fatal("rejected append changed state")
	}
}

func TestConcurrentAppendsHaveOneRecordingOrder(t *testing.T) {
	s, _, _, p := fixture(t, DefaultLimits())
	defer s.Shutdown()
	id := start(t, s, p).ID
	ctx := context.Background()
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if _, err := s.Append(ctx, p, id, Input{Kind: Annotated, Source: "operator", Note: "concurrent"}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	data, err := s.Export(ctx, p, id, ExportAuthorised)
	if err != nil {
		t.Fatal(err)
	}
	var doc AuthorisedExport
	if json.Unmarshal(data, &doc) != nil || len(doc.Entries) != 21 {
		t.Fatal("concurrent data lost")
	}
	for i, e := range doc.Entries {
		if e.Sequence != uint64(i+1) {
			t.Fatal("concurrent ordering is ambiguous")
		}
	}
}
