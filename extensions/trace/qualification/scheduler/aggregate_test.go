package scheduler

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
)

func observe(t *testing.T, a *Aggregate, events ...Event) Snapshot {
	t.Helper()
	for _, e := range events {
		if err := a.Observe(e); err != nil {
			t.Fatal(err)
		}
	}
	s, err := a.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestWakePreemptionSleepExitAndReuse(t *testing.T) {
	a, _ := New(8)
	s := observe(t, a,
		Event{Kind: Wake, Time: 100, PID: 10},
		Event{Kind: Switch, Time: 108, NextPID: 10},
		Event{Kind: Switch, Time: 110, PrevPID: 10, NextPID: 20, PrevRunnable: true},
		Event{Kind: Switch, Time: 125, PrevPID: 20, NextPID: 10},
		Event{Kind: Switch, Time: 130, PrevPID: 10},
		Event{Kind: Wake, Time: 200, PID: 10},
		Event{Kind: Exit, Time: 201, PID: 10},
		Event{Kind: NewTask, Time: 300, PID: 10},
		Event{Kind: Switch, Time: 302, NextPID: 10})
	if s.Histogram.Count != 3 || s.Histogram.SumNanos != 25 || s.Histogram.MaximumNanos != 15 || s.ExitedPending != 1 || s.UnmatchedSwitchIns != 1 || s.Pending != 0 {
		t.Fatal(s)
	}
	if s.Histogram.Buckets[4] != 2 || s.Histogram.Buckets[2] != 1 {
		t.Fatal(s.Histogram)
	}
	// No task identity appears in the only serialisable observation.
	data, _ := json.Marshal(s)
	if strings.Contains(string(data), "PID") || strings.Contains(string(data), "pendingTasks") {
		t.Fatal("task identities retained in output")
	}
}

func TestWakeUsesLatestEnqueueAndRetainsPendingCoverage(t *testing.T) {
	a, _ := New(2)
	s := observe(t, a, Event{Kind: Wake, Time: 10, PID: 123}, Event{Kind: Wake, Time: 12, PID: 123}, Event{Kind: Switch, Time: 20, NextPID: 123}, Event{Kind: Wake, Time: 21, PID: 456})
	if s.ReplacedEnqueues != 1 || s.Histogram.SumNanos != 8 || s.Pending != 1 {
		t.Fatal(s)
	}
}

func TestLossOrderingCapacityAndReuseRemainFatal(t *testing.T) {
	for _, scenario := range []string{"loss", "order", "capacity", "reuse", "invalid"} {
		t.Run(scenario, func(t *testing.T) {
			a, _ := New(1)
			observe(t, a, Event{Kind: Wake, Time: 10, PID: 1})
			var err error
			switch scenario {
			case "loss":
				a.Lost()
			case "order":
				err = a.Observe(Event{Kind: Exit, Time: 9, PID: 1})
			case "capacity":
				err = a.Observe(Event{Kind: Wake, Time: 11, PID: 2})
			case "reuse":
				err = a.Observe(Event{Kind: NewTask, Time: 11, PID: 1})
			case "invalid":
				err = a.Observe(Event{Kind: Switch, Time: 11, PID: 3, NextPID: 1})
			}
			if scenario != "loss" && err == nil {
				t.Fatal("incomplete stream accepted")
			}
			if _, err := a.Snapshot(); err == nil {
				t.Fatal("partial histogram presented as complete")
			}
			if a.Observe(Event{Kind: Exit, Time: 12, PID: 1}) == nil {
				t.Fatal("failure silently recovered")
			}
		})
	}
}

func TestQuantilesReportBucketBoundsIncludingZeroAndMaximum(t *testing.T) {
	h := Histogram{Count: 5}
	h.Buckets[0] = 1
	h.Buckets[1] = 1
	h.Buckets[4] = 2
	h.Buckets[64] = 1
	for _, c := range []struct{ p, lo, hi uint64 }{{1, 0, 0}, {20, 0, 0}, {21, 1, 1}, {50, 8, 15}, {80, 8, 15}, {99, 1 << 63, math.MaxUint64}} {
		b, err := h.Quantile(c.p)
		if err != nil || b.Lower != c.lo || b.Upper != c.hi {
			t.Fatal(c, b, err)
		}
	}
	for _, invalid := range []Histogram{{}, {Count: 1}, {Count: 1, Buckets: [65]uint64{2}}, {Count: 100000001}} {
		if _, err := invalid.Quantile(50); err == nil {
			t.Fatal("invalid histogram accepted")
		}
	}
	for _, p := range []uint64{0, 101} {
		if _, err := h.Quantile(p); err == nil {
			t.Fatal("invalid percentile accepted")
		}
	}
}

func TestBoundsAndOverflowAreNotWrapped(t *testing.T) {
	for _, limit := range []int{0, -1, 32769} {
		if _, err := New(limit); err == nil {
			t.Fatal("unbounded task map")
		}
	}
	a, _ := New(1)
	a.snapshot.Histogram.SumNanos = math.MaxUint64
	observe(t, a, Event{Kind: Wake, Time: 1, PID: 1})
	if a.Observe(Event{Kind: Switch, Time: 2, NextPID: 1}) == nil {
		t.Fatal("sum overflow")
	}
	a, _ = New(1)
	a.snapshot.Events = 100000000
	if a.Observe(Event{Kind: Wake, Time: 1, PID: 1}) == nil {
		t.Fatal("event ceiling exceeded")
	}
}

func TestEventsCannotBeSerialisedOrFormattedAsTaskEvidence(t *testing.T) {
	e := Event{Kind: Wake, Time: 100, PID: 123456789}
	if _, err := json.Marshal(e); err == nil {
		t.Fatal("raw task evidence encoded")
	}
	for _, text := range []string{fmt.Sprint(e), fmt.Sprintf("%+v", e), fmt.Sprintf("%#v", e)} {
		if strings.Contains(text, "123456789") {
			t.Fatal("task identifier formatted")
		}
	}
}
