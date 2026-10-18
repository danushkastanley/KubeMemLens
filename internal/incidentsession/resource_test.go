package incidentsession

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

func TestResourceDecoderBindsScopeAndRequiredStatus(t *testing.T) {
	s, _, _, p := fixture(t, DefaultLimits())
	defer s.Shutdown()
	summary := start(t, s, p)
	resource := Resource{APIVersion: api.MemoryAPIGroup + "/" + api.MemoryAPIVersion, Kind: "IncidentSession", SchemaVersion: 1, Session: summary}
	resource.Metadata.Name = summary.ID
	resource.Metadata.Namespace = p.Namespace
	data, err := json.Marshal(resource)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := DecodeResource(data, p.Namespace, summary.ID); err != nil || got.ID != summary.ID {
		t.Fatal("valid status rejected", err)
	}
	for _, bad := range []string{
		strings.Replace(string(data), `"schemaVersion":1`, `"schemaVersion":2`, 1),
		strings.Replace(string(data), `"limitReached":false,`, "", 1),
		strings.Replace(string(data), `"limitReached":false`, `"limitReached":null`, 1),
		strings.Replace(string(data), `"clockUncertain":false`, `"clockUncertain":null`, 1),
		strings.Replace(string(data), `"sequence":1`, `"sequence":2`, 1),
		strings.Replace(string(data), `"kind":"opened"`, `"kind":"annotated"`, 1),
		strings.Replace(string(data), `"source":"operator"`, `"source":"unknown"`, 1),
		strings.Replace(string(data), `"schemaVersion":1`, `"schemaVersion":1,"schemaVersion":1`, 1),
		strings.Repeat(" ", MaxResourceBytes+1),
	} {
		if got, err := DecodeResource([]byte(bad), p.Namespace, summary.ID); err == nil || got.ID != "" {
			t.Fatal("invalid status accepted")
		}
	}
	if _, err := DecodeResource(data, "other", summary.ID); err == nil {
		t.Fatal("retargeted namespace accepted")
	}
	if _, err := DecodeResource(data, p.Namespace, strings.Repeat("a", 32)); err == nil {
		t.Fatal("retargeted session accepted")
	}
}

func TestCaptureAliasesMatchPublicProjection(t *testing.T) {
	s, clk, _, p := fixture(t, DefaultLimits())
	defer s.Shutdown()
	id := start(t, s, p).ID
	before, after := retainedPair(t, s, p, id, clk.Now(), func(*api.IncidentBundle) {})
	doc := captureExport(t, s, p, id)
	for selector, want := range map[string]string{"evidence-1": before, "evidence-2": after, before: before} {
		value, err := SelectCapture(doc, selector)
		if err != nil || value.Digest != want {
			t.Fatal("alias resolution mismatch", err)
		}
		value.Data[0] = 'x'
	}
	if _, err := SelectCapture(doc, "evidence-3"); err == nil {
		t.Fatal("missing capture invented")
	}
	if _, err := SelectCapture(doc, "evidence-1"); err != nil {
		t.Fatal("selection shared mutable bytes", err)
	}
}

func TestOfflineSanitisationDoesNotAliasPrivateTimes(t *testing.T) {
	s, clk, _, p := fixture(t, DefaultLimits())
	defer s.Shutdown()
	id := start(t, s, p).ID
	if _, err := s.Close(context.Background(), p, id); err != nil {
		t.Fatal(err)
	}
	private := captureExport(t, s, p, id)
	public, err := Sanitise(private)
	if err != nil {
		t.Fatal(err)
	}
	*public.ClosedAt = public.ClosedAt.Add(time.Hour)
	*public.Entries[0].ObservedAt = public.Entries[0].ObservedAt.Add(time.Hour)
	if !private.ClosedAt.Equal(clk.Now()) || !private.Entries[0].ObservedAt.Equal(clk.Now()) {
		t.Fatal("public projection aliases private document")
	}
}
