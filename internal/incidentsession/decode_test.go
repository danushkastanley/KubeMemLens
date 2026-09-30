package incidentsession

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func exportFixture(t *testing.T, op Operation) []byte {
	t.Helper()
	s, clk, _, p := fixture(t, DefaultLimits())
	defer s.Shutdown()
	s.newID = func() (string, error) { return "0123456789abcdef0123456789abcdef", nil }
	id := start(t, s, p).ID
	ctx := context.Background()
	at := clk.Now()
	before := CaptureReference{p.NamespaceUID, strings.Repeat("a", 64), 6, at.Add(-time.Minute)}
	after := CaptureReference{p.NamespaceUID, strings.Repeat("b", 64), 6, at.Add(-30 * time.Second)}
	inputs := []Input{
		{Kind: Annotated, Source: "operator", Note: "Investigate memory growth"},
		{Kind: Captured, Source: "collector", ObservedAt: before.ObservedAt, References: []CaptureReference{before}},
		{Kind: Marked, Source: "collector", ObservedAt: before.ObservedAt, References: []CaptureReference{before}},
		{Kind: Compared, Source: "collector", ObservedAt: at.Add(4 * time.Second), References: []CaptureReference{before, after}},
		{Kind: Gap, Source: "prometheus", GapReason: "source-unavailable"},
	}
	for _, in := range inputs {
		clk.Step(time.Second)
		if _, err := s.Append(ctx, p, id, in); err != nil {
			t.Fatal(err)
		}
	}
	clk.Step(time.Second)
	if _, err := s.Close(ctx, p, id); err != nil {
		t.Fatal(err)
	}
	data, err := s.Export(ctx, p, id, op)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestInitialExportCompatibilityFixtures(t *testing.T) {
	for _, tc := range []struct {
		name string
		op   Operation
	}{{"authorised-v1", ExportAuthorised}, {"sanitised-v1", ExportSanitised}} {
		t.Run(tc.name, func(t *testing.T) {
			data := exportFixture(t, tc.op)
			golden, err := os.ReadFile("testdata/" + tc.name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var compact bytes.Buffer
			if json.Compact(&compact, golden) != nil || !bytes.Equal(data, compact.Bytes()) {
				t.Fatal("versioned export changed")
			}
			doc, err := DecodeExport(golden)
			if err != nil || (doc.Authorised != nil) != (tc.op == ExportAuthorised) || (doc.Sanitised != nil) != (tc.op == ExportSanitised) {
				t.Fatal("compatible export rejected", err)
			}
			var fields map[string]json.RawMessage
			if json.Unmarshal(golden, &fields) != nil {
				t.Fatal("fixture decode")
			}
			reordered, err := json.MarshalIndent(fields, "", " ")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeExport(reordered); err != nil {
				t.Fatal("legal whitespace/key order rejected")
			}
		})
	}
	// The old v2-without-trace fixture remains invalid; v3 is still unsupported.
	for _, name := range []string{"unsupported-v0", "unknown-future-v2", "unknown-future-v3"} {
		data, err := os.ReadFile("testdata/" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeExport(data); !errors.Is(err, ErrInvalid) {
			t.Fatal("unsupported version accepted")
		}
	}
}

func TestExportSyntaxFailsClosed(t *testing.T) {
	private := exportFixture(t, ExportAuthorised)
	public := exportFixture(t, ExportSanitised)
	change := func(data []byte, old, replacement string) []byte {
		if !bytes.Contains(data, []byte(old)) {
			t.Fatal("missing mutation target")
		}
		return bytes.Replace(data, []byte(old), []byte(replacement), 1)
	}
	cases := map[string][]byte{
		"missing privacy":             change(private, `"redacted":false,`, ``),
		"null privacy":                change(private, `"redacted":false`, `"redacted":null`),
		"missing limit state":         change(private, `,"limitReached":false`, ``),
		"missing clock state":         change(private, `"clockUncertain":false,`, ``),
		"missing unknown observation": change(private, `"observedAt":null,`, ``),
		"case alias":                  change(private, `"schemaVersion":1`, `"SchemaVersion":1`),
		"duplicate":                   change(private, `"schemaVersion":1`, `"schemaVersion":1,"schemaVersion":1`),
		"escaped duplicate":           change(private, `"kind":"IncidentSession"`, `"kind":"IncidentSession","\u006bind":"IncidentSession"`),
		"unknown policy":              change(private, `"schemaVersion":1`, `"schemaVersion":1,"permitShared":true`),
		"hybrid privacy":              change(private, `"redacted":false`, `"redacted":true`),
		"private note in public":      change(public, `"kind":"annotated"`, `"kind":"annotated","note":"private"`),
		"private identity in public":  change(public, `"namespace":"namespace-1"`, `"namespace":"namespace-1","namespaceUID":"private"`),
		"invalid UTF16":               change(private, `Investigate memory growth`, `\ud800`),
		"entry whitespace budget":     change(private, `"note":`, strings.Repeat(" ", MaxEntryBytes)+`"note":`),
		"trailing":                    append(append([]byte(nil), private...), []byte(` {}`)...),
		"oversized":                   bytes.Repeat([]byte(" "), MaxExportBytes+1),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeExport(data); !errors.Is(err, ErrInvalid) {
				t.Fatal("malformed export accepted")
			}
		})
	}
}

func TestExportSemanticTamperingRejected(t *testing.T) {
	data := exportFixture(t, ExportAuthorised)
	cases := map[string]func(*AuthorisedExport){
		"order":                   func(d *AuthorisedExport) { d.Entries[2].Sequence = 1 },
		"owner":                   func(d *AuthorisedExport) { d.Entries[2].Actor = "foreign" },
		"tenant":                  func(d *AuthorisedExport) { d.Entries[2].NamespaceUID = "foreign" },
		"reference tenant":        func(d *AuthorisedExport) { d.Entries[2].References[0].NamespaceUID = "foreign" },
		"reference contradiction": func(d *AuthorisedExport) { d.Entries[3].References[0].ObservedAt = d.OpenedAt },
		"close mismatch":          func(d *AuthorisedExport) { at := d.OpenedAt; d.ClosedAt = &at },
		"missing close":           func(d *AuthorisedExport) { d.ClosedAt = nil },
		"late lifecycle":          func(d *AuthorisedExport) { d.Entries[3].Kind = Opened },
		"lifetime":                func(d *AuthorisedExport) { d.ExpiresAt = d.OpenedAt.Add(5 * time.Hour) },
		"hidden clock":            func(d *AuthorisedExport) { at := d.ExpiresAt.Add(time.Second); d.Entries[2].ObservedAt = &at },
		"trace before schema":     func(d *AuthorisedExport) { d.Entries[2].Kind = Kind("trace-reference") },
		"hidden annotations":      func(d *AuthorisedExport) { d.Entries[2].Note = "private" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			var doc AuthorisedExport
			if json.Unmarshal(data, &doc) != nil {
				t.Fatal("fixture")
			}
			mutate(&doc)
			bad, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeExport(bad); !errors.Is(err, ErrInvalid) {
				t.Fatal("contradictory export accepted")
			}
		})
	}
}

func TestPublicAliasesAreBoundedAndConsistent(t *testing.T) {
	data := exportFixture(t, ExportSanitised)
	for _, mutate := range []func(*SanitisedExport){
		func(d *SanitisedExport) { d.Entries[2].References[0].Alias = "private-path" },
		func(d *SanitisedExport) { d.Entries[2].References[0].Alias = "evidence-2" },
		func(d *SanitisedExport) { d.Entries[3].References[0].ObservedAt = d.OpenedAt },
		func(d *SanitisedExport) { d.Entries[2].Actor = "operator-2" },
		func(d *SanitisedExport) { d.Entries[2].Namespace = "private" },
		func(d *SanitisedExport) { d.Entries[2].Sensitivity = "private" },
	} {
		var doc SanitisedExport
		if json.Unmarshal(data, &doc) != nil {
			t.Fatal("fixture")
		}
		mutate(&doc)
		bad, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeExport(bad); !errors.Is(err, ErrInvalid) {
			t.Fatal("public identity contradiction accepted")
		}
	}
}
