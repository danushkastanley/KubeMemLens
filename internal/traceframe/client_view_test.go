package traceframe

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func TestClientClaimMatchesPublicLifetimeWithoutFabricatingCgroup(t *testing.T) {
	_, frame, _ := aggregateFixture(t, trace.Files)
	m, err := frame.ClientMetadata()
	if err != nil {
		t.Fatal(err)
	}
	claim := m.ClientClaim
	claim.ContainerStartedAt = claim.ContainerStartedAt.In(time.FixedZone("equivalent", 3600))
	matched, err := frame.MatchClientClaim(claim)
	if err != nil || matched.BindingDigest == "" || matched.Deadline.IsZero() {
		t.Fatal("public claim mismatch", err)
	}
	for _, change := range []func(*ClientClaim){
		func(c *ClientClaim) { c.PodUID = "replacement" }, func(c *ClientClaim) { c.ContainerStartedAt = c.ContainerStartedAt.Add(time.Second) },
		func(c *ClientClaim) { c.SessionID = strings.Repeat("f", 32) }, func(c *ClientClaim) { c.EngineDigest = "changed" }, func(c *ClientClaim) { c.ProgrammeDigest = "changed" },
		func(c *ClientClaim) { c.Bounds.Events-- }, func(c *ClientClaim) { c.Paths = trace.ConfirmedPaths }, func(c *ClientClaim) { c.Kind = trace.Cache },
	} {
		candidate := claim
		change(&candidate)
		if _, err := frame.MatchClientClaim(candidate); err == nil {
			t.Fatal("changed claim accepted")
		}
	}
	if _, err := json.Marshal(m); err == nil {
		t.Fatal("metadata accidentally serialised")
	}
	if strings.Contains(fmt.Sprintf("%+v", m), "pod-uid") {
		t.Fatal("formatted metadata leaked identity")
	}
}

func TestSummaryProjectionOwnsNumericValuesAndOmitsEvents(t *testing.T) {
	_, _, summary := aggregateFixture(t, trace.Files)
	frame, err := NewSummaryVersion(summary, AggregateVersion)
	if err != nil {
		t.Fatal(err)
	}
	first, err := frame.ClientSummary()
	if err != nil {
		t.Fatal(err)
	}
	*first.EngineCounts.Produced = 999
	*first.Aggregates.Reads.CompletedBytes.Value = 999
	second, err := frame.ClientSummary()
	if err != nil || *second.EngineCounts.Produced != 1 || *second.Aggregates.Reads.CompletedBytes.Value != 8 {
		t.Fatal("mutable summary alias", err)
	}
	if _, err := metadata(t).ClientSummary(); err == nil {
		t.Fatal("metadata accepted as summary")
	}
	if _, err := frame.ClientMetadata(); err == nil {
		t.Fatal("summary accepted as metadata")
	}
}
