package historyview

import (
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

func TestPlotPreservesGapsAndStalePointsWhenCompressed(t *testing.T) {
	zero, high := uint64(0), uint64(100)
	points := []memoryhistory.Point{{Bytes: &zero, State: memoryhistory.Fresh}, {State: memoryhistory.Missing}, {Bytes: &high, State: memoryhistory.Fresh}, {Bytes: &high, State: memoryhistory.Stale}}
	if got := Sparkline(points, 2); got != ".!" {
		t.Fatalf("compressed plot=%q", got)
	}
	if got := Sparkline([]memoryhistory.Point{{Bytes: &zero, State: memoryhistory.Fresh}}, 1); got != "▁" {
		t.Fatal("zero became missing")
	}
}

func TestPresentationAgesWithoutMutatingRetainedEvidence(t *testing.T) {
	now := time.Now().UTC()
	value := uint64(10)
	r := memoryhistory.Report{ReceivedAt: now, State: memoryhistory.Fresh, Completeness: memoryhistory.Complete, Query: memoryhistory.Query{Source: memoryhistory.Prometheus, Metric: memoryhistory.WorkingSet, Start: now, End: now, Step: time.Minute}, Series: []memoryhistory.Series{{Freshness: memoryhistory.Fresh, Points: []memoryhistory.Point{{At: now, SampledAt: now, Bytes: &value, State: memoryhistory.Fresh}}}}}
	text := strings.Join(Lines(r, now.Add(3*time.Minute), 80), "\n")
	if !strings.Contains(text, "Freshness: stale") || !strings.Contains(text, "refresh manually") {
		t.Fatal("historical snapshot appeared live")
	}
	if r.Series[0].Freshness != memoryhistory.Fresh || !r.ReceivedAt.Equal(now) {
		t.Fatal("rendering changed the stored report")
	}
}
