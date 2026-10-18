package traceadmission

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceaudit"
)

func auditFields(t *testing.T, record AuditEvent) map[string]any {
	t.Helper()
	data, err := record.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	return fields
}
func TestAuditCorrelatesOwnerLimitsAndTerminalWithoutPayload(t *testing.T) {
	h := newHarness(t, DefaultPolicy())
	p := actor("private-user")
	a, err := h.manager.Admit(context.Background(), p, requestFor(t, "tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	lease, err := h.manager.Claim(context.Background(), p, "tenant-a", a.ID())
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.RecordTerminal(trace.EventLimit, traceaudit.StreamOutcome); err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	records := append([]AuditEvent(nil), h.audits...)
	h.mu.Unlock()
	actorRef, _ := h.manager.deps.AuditReferences.Actor(p.Name, p.UID)
	tenantRef, _ := h.manager.deps.AuditReferences.Tenant("tenant-a")
	targetRef, _ := h.manager.deps.AuditReferences.Target(a.Specification().Target())
	sessionRef, _ := h.manager.deps.AuditReferences.Session(a.ID())
	terminal, cleanup := 0, 0
	for _, record := range records {
		event := auditFields(t, record)
		if event["operation"] == "configure" {
			continue
		}
		for field, want := range map[string]string{"actorRef": actorRef, "tenantRef": tenantRef, "targetRef": targetRef, "sessionRef": sessionRef} {
			if event[field] != want {
				t.Fatal("lifecycle lost immutable reference", field)
			}
		}
		if event["operation"] == "terminal" {
			terminal++
			if event["reason"] != "event_limit" || event["outcomeSource"] != "validated_stream" {
				t.Fatal("cleanup replaced observed termination")
			}
		}
		if event["operation"] == "cleanup" && event["reason"] == "cleanup_confirmed" {
			cleanup++
		}
		limits := event["limits"].(map[string]any)
		if limits["events"] != float64(trace.DefaultBounds().Events) || limits["perActor"] != float64(1) || limits["resultRetentionNanos"] != float64(0) {
			t.Fatal("audit widened limits or retained results")
		}
	}
	if terminal != 1 || cleanup != 1 {
		t.Fatal("terminal or cleanup record missing/duplicated", terminal, cleanup)
	}
	for _, secret := range []string{p.Name, p.UID, "private-claim", "tenant-a", a.ID(), a.Specification().Target().ContainerID} {
		if strings.Contains(h.auditText(), secret) {
			t.Fatal("audit retained a private identity")
		}
	}
}
func TestAuditFailureCompensatesAdmissionAndKeepsCleanupAvailable(t *testing.T) {
	h := newHarness(t, DefaultPolicy())
	sink := h.manager.deps.Audit
	h.manager.deps.Audit = func(ctx context.Context, record AuditEvent) error {
		event := auditFields(t, record)
		if event["operation"] == "create" {
			return errors.New("private sink token")
		}
		return sink(ctx, record)
	}
	a, err := h.manager.Admit(context.Background(), actor("private-user"), requestFor(t, "tenant-a"))
	if err != ErrUnavailable || a.ID() != "" || !h.binder.first().closed.Load() {
		t.Fatal("failed audit exposed admission or leaked binding")
	}
	if h.manager.AuditHealthy() == nil {
		t.Fatal("audit failure not reflected in readiness")
	}
	calls := h.binder.calls.Load()
	if _, err := h.manager.Admit(context.Background(), actor("other"), requestFor(t, "tenant-b")); err != ErrUnavailable || h.binder.calls.Load() != calls {
		t.Fatal("audit failure allowed more node work")
	}
}
func TestAuditFailureDoesNotPreventPhysicalCancellation(t *testing.T) {
	h := newHarness(t, DefaultPolicy())
	p := actor("user-a")
	a, err := h.manager.Admit(context.Background(), p, requestFor(t, "tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	h.manager.auditFailed.Store(true)
	if err := h.manager.Cancel(context.Background(), p, "tenant-a", a.ID()); err != ErrUnavailable {
		t.Fatal("audit failure was hidden")
	}
	if !h.binder.first().closed.Load() {
		t.Fatal("audit failure prevented physical cleanup")
	}
}
func TestAdmissionExpiryDuringAuditNeverReturnsAUsableAdmission(t *testing.T) {
	h := newHarness(t, DefaultPolicy())
	sink := h.manager.deps.Audit
	h.manager.deps.Audit = func(ctx context.Context, record AuditEvent) error {
		event := auditFields(t, record)
		if event["operation"] == "create" {
			h.manager.mu.Lock()
			for _, e := range h.manager.entries {
				e.expires = time.Now().Add(-time.Second)
			}
			h.manager.mu.Unlock()
		}
		return sink(ctx, record)
	}
	a, err := h.manager.Admit(context.Background(), actor("user-a"), requestFor(t, "tenant-a"))
	if err != ErrExpired || a.ID() != "" || !h.binder.first().closed.Load() {
		t.Fatal("audit delay returned expired authority", err)
	}
}

func TestClaimAuditFailureReturnsNoLeaseAndReleasesBinding(t *testing.T) {
	h := newHarness(t, DefaultPolicy())
	p := actor("user-a")
	a, err := h.manager.Admit(context.Background(), p, requestFor(t, "tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	sink := h.manager.deps.Audit
	h.manager.deps.Audit = func(ctx context.Context, record AuditEvent) error {
		if auditFields(t, record)["operation"] == "stream" {
			return errors.New("audit sink unavailable")
		}
		return sink(ctx, record)
	}
	lease, err := h.manager.Claim(context.Background(), p, "tenant-a", a.ID())
	if err != ErrUnavailable || lease != nil || !h.binder.first().closed.Load() {
		t.Fatal("failed claim audit returned authority or retained binding", err)
	}
	if h.manager.AuditHealthy() == nil {
		t.Fatal("failed claim audit did not latch readiness")
	}
}

func TestActiveAuditFailureStopsDisclosureButAllowsCleanup(t *testing.T) {
	h := newHarness(t, DefaultPolicy())
	p := actor("user-a")
	a, err := h.manager.Admit(context.Background(), p, requestFor(t, "tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	lease, err := h.manager.Claim(context.Background(), p, "tenant-a", a.ID())
	if err != nil {
		t.Fatal(err)
	}
	h.manager.deps.Audit = func(context.Context, AuditEvent) error { return errors.New("audit sink unavailable") }
	got, err := h.manager.Get(context.Background(), p, "tenant-a", a.ID())
	if err != ErrUnavailable || got.ID() != "" {
		t.Fatal("failed audit disclosed admission", err)
	}
	if err := lease.RevalidateStream(context.Background()); err != ErrUnavailable {
		t.Fatal("audit failure permitted stream disclosure", err)
	}
	if err := lease.RecordTerminal(trace.Expired, traceaudit.StreamOutcome); err != ErrUnavailable {
		t.Fatal("audit failure permitted terminal disclosure", err)
	}
	_ = lease.Close(context.Background())
	if !h.binder.first().closed.Load() {
		t.Fatal("audit failure prevented physical cleanup")
	}
}
