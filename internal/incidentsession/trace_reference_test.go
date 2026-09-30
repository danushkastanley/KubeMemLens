package incidentsession

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/tracereport"
)

func traceReportFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../tracereport/testdata/schema2-v2-file-loss.json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestTraceReferenceRecordsOnlyTypedOperatorReceipt(t *testing.T) {
	s, _, policy, p := fixture(t, DefaultLimits())
	defer s.Shutdown()
	ctx := context.Background()
	id := start(t, s, p).ID
	data := traceReportFixture(t)
	var original map[string]any
	_ = json.Unmarshal(data, &original)
	original["toolVersion"] = "private-identity-secret"
	original["caveats"] = []string{"private-path-secret"}
	data, _ = json.Marshal(original)
	reference, err := tracereport.Describe(data)
	if err != nil {
		t.Fatal(err)
	}
	value, err := s.RecordTraceReference(ctx, p, id, reference)
	if err != nil || value.Latest.Kind != TraceReferenced || value.Latest.Source != traceReferenceSource || value.Entries != 2 {
		t.Fatal("reference not appended", err)
	}
	if policy.calls[len(policy.calls)-1] != ReferenceTrace {
		t.Fatal("reference action used an unrelated permission")
	}
	for _, mode := range []Operation{ExportAuthorised, ExportSanitised} {
		encoded, err := s.Export(ctx, p, id, mode)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(encoded, []byte("private-identity-secret")) || bytes.Contains(encoded, []byte("private-path-secret")) || bytes.Contains(encoded, []byte(`"data":`)) {
			t.Fatal("raw report text or bytes retained")
		}
		decoded, err := DecodeExport(encoded)
		if err != nil {
			t.Fatal("own trace-bearing export unreadable", err)
		}
		if mode == ExportSanitised {
			if decoded.Sanitised.SchemaVersion != TraceExportSchema || decoded.Sanitised.Entries[1].TraceReference.Alias != "trace-1" ||
				decoded.Sanitised.Entries[1].TraceReference.Loss != "reported" || bytes.Contains(encoded, []byte("digest")) || bytes.Contains(encoded, []byte(p.NamespaceUID)) {
				t.Fatal("sanitised export lost uncertainty or disclosed identity")
			}
			continue
		}
		if decoded.Authorised.SchemaVersion != TraceExportSchema || len(decoded.Authorised.Captures) != 0 {
			t.Fatal("wrong representation or report stored as a capture")
		}
		ref, err := SelectTraceReference(*decoded.Authorised, "trace-1")
		if err != nil || tracereport.VerifyReference(ref, data) != nil {
			t.Fatal("reference cannot verify retained local report", err)
		}
		if _, err := SelectCapture(*decoded.Authorised, ref.Digest); !errors.Is(err, ErrNotFound) {
			t.Fatal("trace reference became retained server evidence")
		}
		ref.Provenance = "forged"
		again, err := s.Export(ctx, p, id, mode)
		if err != nil || !bytes.Equal(encoded, again) {
			t.Fatal("returned reference shared mutable state")
		}
	}
}

func TestTraceReferenceAuthorityLifetimeAndMalformedInput(t *testing.T) {
	s, clk, policy, p := fixture(t, DefaultLimits())
	defer s.Shutdown()
	ctx := context.Background()
	id := start(t, s, p).ID
	data := traceReportFixture(t)
	ref, err := tracereport.Describe(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, other := range []Principal{{p.Namespace, p.NamespaceUID, "other"}, {"other", "other-namespace", p.Actor}, {p.Namespace, "replacement-uid", p.Actor}} {
		if _, err := s.RecordTraceReference(ctx, other, id, ref); !errors.Is(err, ErrNotFound) {
			t.Fatal("foreign actor or namespace attached evidence", err)
		}
	}
	policy.deny = true
	if _, err := s.RecordTraceReference(ctx, p, id, tracereport.Reference{}); !errors.Is(err, ErrDenied) {
		t.Fatal("reference was inspected before authorisation", err)
	}
	policy.deny = false
	oversized := ref
	oversized.Bytes = tracereport.MaxBytes + 1
	for _, bad := range []tracereport.Reference{{}, {SchemaVersion: 99}, oversized} {
		if _, err := s.RecordTraceReference(ctx, p, id, bad); !errors.Is(err, ErrInvalid) {
			t.Fatal("malformed report accepted", err)
		}
	}
	if _, err := s.Append(ctx, p, id, Input{Kind: TraceReferenced, Source: traceReferenceSource, ObservedAt: ref.CapturedAt, traceReference: &ref}); !errors.Is(err, ErrInvalid) {
		t.Fatal("generic append bypassed trace-reference authority")
	}
	status, err := s.Inspect(ctx, p, id)
	if err != nil || status.Entries != 1 || status.LimitReached {
		t.Fatal("rejected input mutated session")
	}
	if _, err := s.RecordTraceReference(ctx, p, id, ref); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, p, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Export(ctx, p, id, ExportAuthorised); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted reference retained")
	}
	id = start(t, s, p).ID
	clk.Step(DefaultLimits().Retention)
	if _, err := s.RecordTraceReference(ctx, p, id, ref); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired session revived")
	}
}

func TestTraceTimelineReservesReadableCloseWithinExistingParserBudget(t *testing.T) {
	s, _, _, p := fixture(t, DefaultLimits())
	defer s.Shutdown()
	ctx := context.Background()
	id := start(t, s, p).ID
	ref, err := tracereport.Describe(traceReportFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	added := 0
	for range MaxEntries {
		_, err := s.RecordTraceReference(ctx, p, id, ref)
		if errors.Is(err, ErrCapacity) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		added++
	}
	if added == 0 || added >= MaxEntries-2 {
		t.Fatal("dense references did not exercise existing token ceiling", added)
	}
	closed, err := s.Close(ctx, p, id)
	if err != nil || closed.ClosedAt == nil || !closed.LimitReached {
		t.Fatal("close reserve lost", err)
	}
	for _, mode := range []Operation{ExportAuthorised, ExportSanitised} {
		encoded, err := s.Export(ctx, p, id, mode)
		if err != nil || len(encoded) > MaxExportBytes {
			t.Fatal("export exceeds existing bound", err)
		}
		if _, err := DecodeExport(encoded); err != nil {
			t.Fatal("full closed timeline unreadable", err)
		}
	}
}

func TestTraceExportCannotMasqueradeAsLegacyOrChangeReceipt(t *testing.T) {
	s, _, _, p := fixture(t, DefaultLimits())
	defer s.Shutdown()
	ctx := context.Background()
	id := start(t, s, p).ID
	ref, err := tracereport.Describe(traceReportFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := s.RecordTraceReference(ctx, p, id, ref); err != nil {
			t.Fatal(err)
		}
	}
	data, _ := s.Export(ctx, p, id, ExportAuthorised)
	for _, change := range []func(*AuthorisedExport){
		func(d *AuthorisedExport) { d.SchemaVersion = SchemaVersion },
		func(d *AuthorisedExport) {
			d.Entries[2].TraceReference.Digest = strings.Repeat("a", 64)
			d.Entries[2].TraceReference.Provenance = "verified"
		},
		func(d *AuthorisedExport) { d.Entries[2].TraceReference.Bytes++ },
		func(d *AuthorisedExport) { d.Entries[1].TraceReference = nil },
		func(d *AuthorisedExport) { d.Entries[0].TraceReference = d.Entries[1].TraceReference },
	} {
		var doc AuthorisedExport
		if json.Unmarshal(data, &doc) != nil {
			t.Fatal("own export invalid")
		}
		change(&doc)
		bad, _ := json.Marshal(doc)
		if _, err := DecodeExport(bad); !errors.Is(err, ErrInvalid) {
			t.Fatal("tampered trace export accepted", err)
		}
	}
}
