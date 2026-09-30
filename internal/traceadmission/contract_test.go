package traceadmission

import (
	"errors"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/tracecompat"
)

func TestNegotiatedRequestRetainsExactSelectionAndBounds(t *testing.T) {
	previous, err := decodeSelected(t, selectedRequestBody())
	if err != nil {
		t.Fatal(err)
	}
	body := selectedRequestBody()
	body["schemaVersion"] = tracecompat.RequestSchema
	body["contractVersion"] = 1
	current, err := decodeSelected(t, body)
	if err != nil {
		t.Fatal(err)
	}
	if current.SchemaVersion() != 3 || previous.SchemaVersion() != 2 || *current.selection != *previous.selection || current.Bounds() != previous.Bounds() || current.Kind() != previous.Kind() || current.Paths() != previous.Paths() {
		t.Fatal("contract changed admitted meaning")
	}
	h := newHarness(t, DefaultPolicy())
	h.auth.denied.Store(true)
	if _, err := h.manager.Admit(t.Context(), actor("operator"), current); !errors.Is(err, ErrDenied) {
		t.Fatal("negotiation bypassed authorisation")
	}
	if h.resolver.calls.Load() != 0 || h.binder.calls.Load() != 0 {
		t.Fatal("denied contract reached target/node")
	}
}
func TestNegotiatedRequestRequiresContractAndCannotWidenLimits(t *testing.T) {
	for _, change := range []func(map[string]any){
		func(v map[string]any) { delete(v, "contractVersion") }, func(v map[string]any) { v["contractVersion"] = nil },
		func(v map[string]any) { v["contractVersion"] = 0 }, func(v map[string]any) { v["contractVersion"] = 2 },
		func(v map[string]any) { v["contractVersion"] = true }, func(v map[string]any) { v["contractVersion"] = "1" },
		func(v map[string]any) { v["schemaVersion"] = 2 }, func(v map[string]any) { v["schemaVersion"] = 1 },
		func(v map[string]any) { delete(v, "expectedPodUID") }, func(v map[string]any) { delete(v, "expectedContainerID") },
		func(v map[string]any) { delete(v, "expectedContainerStartedAt") }, func(v map[string]any) { delete(v, "expectedNodeName") },
		func(v map[string]any) { v["maxEvents"] = 100001 }, func(v map[string]any) { v["durationSeconds"] = 301 },
		func(v map[string]any) { v["maxOutputBytes"] = 32<<20 + 1 }, func(v map[string]any) { v["maxMapBytes"] = 32<<20 + 1 },
		func(v map[string]any) { v["maxPathBytes"] = 513 },
	} {
		body := selectedRequestBody()
		body["schemaVersion"] = 3
		body["contractVersion"] = 1
		change(body)
		if _, err := decodeSelected(t, body); !errors.Is(err, ErrInvalidRequest) {
			t.Fatal("ambiguous or over-budget contract accepted")
		}
	}
}
