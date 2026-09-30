package traceaudit

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func eventFixture(t *testing.T) Event {
	t.Helper()
	refs := referenceFixture(t)
	actor, _ := refs.Actor("private-user", "private-uid")
	tenant, _ := refs.Tenant("private-tenant")
	session, _ := refs.Session(strings.Repeat("c", 32))
	b := trace.DefaultBounds()
	return Event{Time: time.Now(), KeyID: refs.KeyID(), PolicyRef: "sha256:" + strings.Repeat("a", 64), ActorClass: "user", ActorRef: actor, TenantRef: tenant, SessionRef: session, TargetRef: "hmac-sha256:" + strings.Repeat("b", 64), Operation: Create, Decision: Accepted, Reason: Admitted, Kind: trace.Files,
		Limits: &Limits{DurationNanos: int64(b.Duration), Events: b.Events, OutputBytes: b.OutputBytes, MapBytes: b.MapBytes, PathBytes: b.PathBytes, PerActor: 1, PerTenant: 2, PerNode: 1, Cluster: 32, PendingTTLNanos: int64(15 * time.Second)}}
}
func TestAuditRecordIsBoundedImmutableAndContainsOnlyReferences(t *testing.T) {
	event := eventFixture(t)
	record, err := NewRecord(event)
	if err != nil {
		t.Fatal(err)
	}
	data, err := record.Bytes()
	if err != nil || len(data) > MaxRecordBytes || !json.Valid(data) {
		t.Fatal("invalid audit document", err)
	}
	for _, private := range []string{"private-user", "private-uid", "private-tenant"} {
		if bytes.Contains(data, []byte(private)) {
			t.Fatal("identity disclosed")
		}
	}
	if !bytes.Contains(data, []byte(`"resultRetentionNanos":0`)) || !bytes.Contains(data, []byte(`"schemaVersion":1`)) || !bytes.Contains(data, []byte(`"actorRef":`)) {
		t.Fatal("missing audit semantics")
	}
	event.ActorRef = "raw-name"
	event.Limits.Events = 0
	data[0] = 'x'
	again, _ := record.Bytes()
	if !json.Valid(again) || bytes.Contains(again, []byte("raw-name")) || bytes.Contains(again, []byte(`"events":0`)) {
		t.Fatal("record shares mutable input or output")
	}
	if _, err := json.Marshal(event); err == nil {
		t.Fatal("unvalidated event bypassed encoder")
	}
}
func TestAuditRejectsPayloadsInvalidDecisionsAndUnsafeLimits(t *testing.T) {
	changes := []func(*Event){
		func(e *Event) { e.ActorRef = "private-user" }, func(e *Event) { e.TargetRef = "/private/path" },
		func(e *Event) { e.ActorClass = "private-user" }, func(e *Event) { e.Reason = "private error payload" },
		func(e *Event) { e.Operation = "private-command" }, func(e *Event) { e.KeyID = "secret-key" },
		func(e *Event) { e.PolicyRef = "unverified" }, func(e *Event) { e.Decision = Rejected; e.Reason = Admitted },
		func(e *Event) { e.TargetRef = "" }, func(e *Event) { e.SessionRef = "" }, func(e *Event) { e.Limits = nil },
		func(e *Event) { e.Limits.Cluster = 65 }, func(e *Event) { e.Limits.ResultRetentionNanos = 1 },
	}
	for _, change := range changes {
		event := eventFixture(t)
		change(&event)
		if _, err := NewRecord(event); err != ErrInvalid {
			t.Fatal("invalid or sensitive audit input accepted")
		}
	}
}
func TestUnauthenticatedDenialDoesNotInventAnActorOrTarget(t *testing.T) {
	event := eventFixture(t)
	event.Decision, event.Reason = Rejected, Unauthenticated
	event.ActorClass, event.ActorRef, event.TargetRef, event.SessionRef = "unauthenticated", "", "", ""
	event.Limits = nil
	if _, err := NewRecord(event); err != nil {
		t.Fatal(err)
	}
	event.ActorRef = "hmac-sha256:" + strings.Repeat("d", 64)
	if _, err := NewRecord(event); err != ErrInvalid {
		t.Fatal("unauthenticated denial invented identity")
	}
}
