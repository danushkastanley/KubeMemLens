package changemarkers

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

func fixture() (memoryhistory.Selection, memoryhistory.Query, Marker) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	s := memoryhistory.Selection{Request: memoryhistory.Request{Scope: memoryhistory.Pod, Namespace: "tenant-a", Name: "app"}, UID: "pod-uid", ResolvedAt: now,
		Targets: []memoryhistory.Target{{Namespace: "tenant-a", Pod: "app", PodUID: "pod-uid", Container: "app", ContainerID: "containerd://instance", Node: "node-a", NodeUID: "node-uid", StartedAt: now.Add(-time.Hour)}}}
	q := memoryhistory.Query{Source: memoryhistory.Prometheus, Metric: memoryhistory.WorkingSet, Start: now.Add(-5 * time.Minute), End: now, Step: time.Minute}
	at := now.Add(-90 * time.Second)
	m := Marker{Kind: Restarted, At: at, Until: at, Clock: ContainerState, SourceUID: s.UID, Subject: Object{"v1", "Pod", "tenant-a", "app", s.UID}, Container: "app", Count: 1}
	return s, q, m
}

func TestSourceTimeAndOwnerEvidenceSurviveAlignment(t *testing.T) {
	s, q, marker := fixture()
	marker.Owners = []Object{{"apps/v1", "ReplicaSet", "tenant-a", "app-rs", "rs-uid"}, {"apps/v1", "Deployment", "tenant-a", "app", "deployment-uid"}}
	original := marker
	r, err := Compose(s, q, s.ResolvedAt, Missing, []Marker{marker}, false)
	if err != nil {
		t.Fatal(err)
	}
	aligned, err := Align(r, q)
	if err != nil || len(aligned) != 1 || aligned[0].First != 3 || aligned[0].Last != 3 || !reflect.DeepEqual(aligned[0].Marker, original) {
		t.Fatalf("source evidence was changed or misplaced: %+v, %v", aligned, err)
	}
	marker.Owners[0].UID = "changed-input"
	aligned[0].Marker.Owners[0].UID = "changed-output"
	if r.Markers[0].Owners[0].UID != "rs-uid" {
		t.Fatal("owner evidence aliases caller memory")
	}
	if r.Events != Missing {
		t.Fatal("Pod state invented event history")
	}
}

func TestIntervalsRetainExactTimeAndUncertainty(t *testing.T) {
	s, q, m := fixture()
	m.Kind, m.Clock, m.Container, m.PreviousUID, m.Uncertain = Replacement, Observation, "", "old-pod-uid", true
	m.At, m.Until = q.Start.Add(-time.Second), q.End.Add(-30*time.Second)
	r, err := Compose(s, q, s.ResolvedAt, Partial, []Marker{m}, false)
	if err != nil {
		t.Fatal(err)
	}
	a, err := Align(r, q)
	if err != nil || a[0].First != 0 || a[0].Last != 4 || !a[0].Marker.At.Equal(m.At) || !a[0].Marker.Uncertain {
		t.Fatalf("interval changed: %+v %v", a, err)
	}
}

func TestCurrentRevisionDoesNotInventRollbackTime(t *testing.T) {
	s, q, _ := fixture()
	s.Request = memoryhistory.Request{Scope: memoryhistory.Workload, Namespace: "tenant-a", Name: "app", WorkloadKind: "Deployment"}
	s.UID = "deployment-uid"
	m := Marker{Kind: Revision, Clock: Observation, At: s.ResolvedAt, Until: s.ResolvedAt, SourceUID: "old-rs-uid", Subject: Object{"apps/v1", "ReplicaSet", "tenant-a", "old-rs", "old-rs-uid"}, Owners: []Object{{"apps/v1", "Deployment", "tenant-a", "app", s.UID}}, Revision: 3, Count: 1, Uncertain: true}
	r, err := Compose(s, q, s.ResolvedAt, Missing, []Marker{m}, false)
	if err != nil {
		t.Fatal(err)
	}
	a, err := Align(r, q)
	if err != nil || a[0].First != -1 || a[0].Last != -1 {
		t.Fatal("current revision was plotted as a timed rollout", a, err)
	}
	m.At, m.Until = q.Start, q.Start
	if _, err := Compose(s, q, s.ResolvedAt, Missing, []Marker{m}, false); err == nil {
		t.Fatal("historical creation timestamp accepted for current revision")
	}
}

func TestReusedNamesAndTenantBoundariesRejectJoins(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Marker)
	}{
		{"same name old UID", func(m *Marker) { m.Subject.UID = "old-uid"; m.SourceUID = "old-uid" }},
		{"other tenant", func(m *Marker) { m.Subject.Namespace = "tenant-b" }},
		{"other container", func(m *Marker) { m.Container = "other" }},
		{"unverified owner namespace", func(m *Marker) { m.Owners = []Object{{"apps/v1", "Deployment", "tenant-b", "app", "owner"}} }},
		{"owner cycle", func(m *Marker) { m.Owners = []Object{m.Subject} }},
		{"future source clock", func(m *Marker) { m.At = m.At.Add(time.Hour); m.Until = m.At }},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, q, m := fixture()
			s.Request.Scope = memoryhistory.Container
			s.Request.Container = "app"
			test.change(&m)
			if _, err := Compose(s, q, s.ResolvedAt, Partial, []Marker{m}, false); err == nil {
				t.Fatal("invalid identity/time evidence joined")
			}
		})
	}
}

func TestMarkerDensityIsBoundedAndDeterministic(t *testing.T) {
	s, q, base := fixture()
	var candidates []Marker
	for i := 0; i < MaxMarkers+10; i++ {
		m := base
		m.Kind = Started
		m.Clock = KubernetesEvent
		m.SourceUID = fmt.Sprintf("event-%03d", i)
		m.At = q.Start.Add(time.Duration(i) * time.Second)
		m.Until = m.At
		candidates = append(candidates, m)
	}
	r, err := Compose(s, q, s.ResolvedAt, Partial, candidates, false)
	if err != nil || len(r.Markers) != MaxMarkers || !r.Truncated || r.Markers[0].SourceUID != "event-010" {
		t.Fatalf("density bound: %+v %v", r, err)
	}
	for i, j := 0, len(candidates)-1; i < j; i, j = i+1, j-1 {
		candidates[i], candidates[j] = candidates[j], candidates[i]
	}
	reversed, err := Compose(s, q, s.ResolvedAt, Partial, candidates, false)
	if err != nil || !reflect.DeepEqual(r, reversed) {
		t.Fatal("API ordering changed retained evidence")
	}
}

func TestMissingAndUncertainEvidenceCannotClaimCompleteness(t *testing.T) {
	s, q, m := fixture()
	for _, state := range []Coverage{Missing, Denied, Unavailable, Disabled, Partial} {
		r, err := Compose(s, q, s.ResolvedAt, state, nil, false)
		if err != nil || r.Events != state {
			t.Fatalf("lost explicit state %s: %v", state, err)
		}
	}
	if _, err := Compose(s, q, s.ResolvedAt, "complete", nil, false); err == nil {
		t.Fatal("best-effort events claimed complete")
	}
	m.Kind, m.Clock, m.Container = Resize, PodCondition, ""
	m.ResizeState = "in-progress"
	if _, err := Compose(s, q, s.ResolvedAt, Missing, []Marker{m}, false); err != nil {
		t.Fatal("condition source unnecessarily required retained events", err)
	}
	m.Clock = KubernetesEvent
	m.ResizeState = "started"
	m.SourceUID = "event-uid"
	if _, err := Compose(s, q, s.ResolvedAt, Missing, []Marker{m}, false); err == nil {
		t.Fatal("missing events contradicted by an event marker")
	}
}
