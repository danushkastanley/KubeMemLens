package traceadmission

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func selectedRequestBody() map[string]any {
	return map[string]any{
		"schemaVersion": 2, "pod": "private-pod", "container": "worker", "kind": "files",
		"expectedPodUID": "uid-tenant-a", "expectedContainerID": strings.Repeat("a", 64),
		"expectedContainerStartedAt": "2026-09-01T00:00:00Z", "expectedNodeName": "node-one",
	}
}

func decodeSelected(t *testing.T, body map[string]any) (Request, error) {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return DecodeRequest("tenant-a", strings.NewReader(string(data)))
}

func TestSelectedLifetimeAdmissionStillUsesResolvedNodeBinding(t *testing.T) {
	body := selectedRequestBody()
	body["expectedContainerStartedAt"] = "2026-09-01T01:00:00+01:00"
	r, err := decodeSelected(t, body)
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, DefaultPolicy())
	a, err := h.manager.Admit(context.Background(), actor("operator"), r)
	if err != nil || h.binder.calls.Load() != 1 {
		t.Fatal("matching selection was not resolved and bound", err)
	}
	target := a.Specification().Target()
	if target.CgroupID != 42 || target.NodeUID != "node-uid" || !r.matchesSelection(testWorkload(r)) {
		t.Fatal("caller precondition replaced server binding authority")
	}
	if strings.Contains(fmt.Sprintf("%+v", r), "uid-tenant-a") {
		t.Fatal("formatted request disclosed selection")
	}
	if _, err := json.Marshal(r); err == nil {
		t.Fatal("ordinary JSON disclosed selection")
	}
}

func TestSelectionChangeRejectsBeforeNodeWorkAndReleasesReservation(t *testing.T) {
	for name, change := range map[string]func(*Workload){
		"Pod replacement":       func(w *Workload) { w.Target.PodUID = "replacement" },
		"container replacement": func(w *Workload) { w.Target.ContainerID = strings.Repeat("b", 64) },
		"container restart":     func(w *Workload) { w.Target.ContainerStartedAt = w.Target.ContainerStartedAt.Add(time.Second) },
		"node movement":         func(w *Workload) { w.NodeName = "node-two" },
	} {
		t.Run(name, func(t *testing.T) {
			r, err := decodeSelected(t, selectedRequestBody())
			if err != nil {
				t.Fatal(err)
			}
			h := newHarness(t, DefaultPolicy())
			h.resolver.resolve = func(_ context.Context, request Request) (Workload, error) {
				w := testWorkload(request)
				change(&w)
				return w, nil
			}
			if _, err := h.manager.Admit(context.Background(), actor("operator"), r); !errors.Is(err, ErrTargetChanged) {
				t.Fatal("changed selection was accepted", err)
			}
			if h.binder.calls.Load() != 0 {
				t.Fatal("changed selection reached node binding")
			}
			h.resolver.resolve = nil
			if _, err := h.manager.Admit(context.Background(), actor("operator"), r); err != nil {
				t.Fatal("failed precondition retained admission quota", err)
			}
		})
	}
}

func TestSelectedLifetimeCannotGrantAccess(t *testing.T) {
	r, err := decodeSelected(t, selectedRequestBody())
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, DefaultPolicy())
	h.auth.denied.Store(true)
	if _, err := h.manager.Admit(context.Background(), actor("operator"), r); !errors.Is(err, ErrDenied) {
		t.Fatal("selection bypassed authorisation", err)
	}
	if h.resolver.calls.Load() != 0 || h.binder.calls.Load() != 0 {
		t.Fatal("denied selection reached target resolution")
	}
}

func TestSelectedRequestRequiresCompleteVersionedPreconditions(t *testing.T) {
	for _, field := range []string{"expectedPodUID", "expectedContainerID", "expectedContainerStartedAt", "expectedNodeName"} {
		for _, mutation := range []string{"missing", "null", "empty"} {
			t.Run(field+"/"+mutation, func(t *testing.T) {
				body := selectedRequestBody()
				switch mutation {
				case "missing":
					delete(body, field)
				case "null":
					body[field] = nil
				case "empty":
					body[field] = ""
				}
				if _, err := decodeSelected(t, body); !errors.Is(err, ErrInvalidRequest) {
					t.Fatal("incomplete precondition accepted", err)
				}
			})
		}
	}
	for _, invalid := range []struct {
		field string
		value any
	}{
		{"schemaVersion", 1}, {"schemaVersion", 3}, {"expectedPodUID", "private\nuid"},
		{"expectedPodUID", strings.Repeat("a", 129)}, {"expectedContainerID", "short"},
		{"expectedContainerStartedAt", "0001-01-01T00:00:00Z"}, {"expectedNodeName", "../other"},
		{"expectedNodeName", strings.Repeat("a", 64) + ".example"},
		{"nodeUID", "caller-authority"}, {"cgroupID", 123},
	} {
		body := selectedRequestBody()
		body[invalid.field] = invalid.value
		if _, err := decodeSelected(t, body); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("invalid %s accepted: %v", invalid.field, err)
		}
	}
}
