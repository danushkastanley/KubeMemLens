package memoryhistory

import (
	"errors"
	"testing"
	"time"
)

func selected() Selection {
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	return Selection{Request: Request{Scope: Pod, Namespace: "tenant-a", Name: "app"}, UID: "pod-uid", ResolvedAt: at,
		Targets: []Target{{Namespace: "tenant-a", Pod: "app", PodUID: "pod-uid", Container: "app", ContainerID: "containerd://instance", Node: "node-a", NodeUID: "node-uid", StartedAt: at.Add(-time.Hour)}}}
}
func query() Query {
	at := selected().ResolvedAt
	return Query{Source: Prometheus, Metric: WorkingSet, Start: at.Add(-2 * time.Minute), End: at, Step: time.Minute}
}

func TestQueryBounds(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Query)
		want   error
	}{
		{"zero step", func(q *Query) { q.Step = 0 }, ErrBounds},
		{"too many points", func(q *Query) { q.Start = q.End.Add(-MaxPoints * time.Minute) }, ErrBounds},
		{"too long", func(q *Query) { q.Start = q.End.Add(-MaxRange - time.Second); q.Step = time.Hour }, ErrBounds},
		{"future", func(q *Query) { q.End = q.End.Add(time.Second) }, ErrInvalid},
		{"reverse", func(q *Query) { q.Start = q.End.Add(time.Second) }, ErrInvalid},
		{"fractional grid", func(q *Query) { q.Start = q.Start.Add(time.Nanosecond) }, ErrInvalid},
		{"unknown source", func(q *Query) { q.Source = "automatic" }, ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := query()
			tc.change(&q)
			if _, err := NewReport(selected(), q, selected().ResolvedAt); !errors.Is(err, tc.want) {
				t.Fatalf("error=%v", err)
			}
		})
	}
	q := query()
	q.Start = q.End.Add(-240 * time.Minute)
	r, err := NewReport(selected(), q, selected().ResolvedAt)
	if err != nil || len(r.Series[0].Points) != MaxPoints {
		t.Fatalf("maximum grid: %v", err)
	}
}

func TestSelectionRejectsUnboundIdentity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Selection)
	}{
		{"other namespace", func(s *Selection) { s.Targets[0].Namespace = "tenant-b" }},
		{"other Pod instance", func(s *Selection) { s.Targets[0].PodUID = "replacement" }},
		{"unknown node identity", func(s *Selection) { s.Targets[0].NodeUID = "" }},
		{"unknown container identity", func(s *Selection) { s.Targets[0].ContainerID = "" }},
		{"future lifetime", func(s *Selection) { s.Targets[0].StartedAt = s.ResolvedAt.Add(time.Second) }},
		{"duplicate", func(s *Selection) { s.Targets = append(s.Targets, s.Targets[0]) }},
		{"too many", func(s *Selection) { s.Targets = make([]Target, MaxTargets+1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := selected()
			tc.change(&s)
			if _, err := NewReport(s, query(), s.ResolvedAt); err == nil {
				t.Fatal("unbound selection accepted")
			}
		})
	}
}

func TestZeroMissingStaleAndHistoricalFreshnessRemainDistinct(t *testing.T) {
	s := selected()
	q := query()
	r, err := NewReport(s, q, s.ResolvedAt)
	if err != nil {
		t.Fatal(err)
	}
	zero := uint64(0)
	r.Series[0].Points[0] = Point{At: q.Start, SampledAt: q.Start, Bytes: &zero, State: Fresh}
	r.Series[0].Points[2] = Point{At: q.End, SampledAt: q.End.Add(-3 * time.Minute), Bytes: &zero, State: Stale}
	Summarise(&r, false)
	if r.Completeness != Partial || r.State != Stale || r.Series[0].Points[1].Bytes != nil || r.Series[0].Points[0].Bytes == nil {
		t.Fatal("missing, zero or age semantics lost")
	}
	if r.Series[0].Points[0].State != Fresh {
		t.Fatal("historical sample lost its evaluation-relative freshness")
	}
}
