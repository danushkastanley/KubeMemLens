package verifier

import (
	"encoding/json"
	"fmt"
	"testing"
)

func feed(t *testing.T, a *Aggregate, events ...Event) {
	t.Helper()
	for _, event := range events {
		if err := a.Observe(event); err != nil {
			t.Fatal(err)
		}
	}
}

func TestConcurrentCallsKeepDurationAndLogResultsTogether(t *testing.T) {
	a := New()
	feed(t, a, Event{Kind: CheckEnter, Time: 10, TID: 1}, Event{Kind: CheckEnter, Time: 11, TID: 2},
		Event{Kind: LogFinalized, Time: 12, TID: 2, LogSizeBytes: 129, Result: -28},
		Event{Kind: LogFinalized, Time: 13, TID: 1}, Event{Kind: CheckReturn, Time: 14, TID: 1},
		Event{Kind: CheckReturn, Time: 20, TID: 2, Result: -22})
	result, err := a.Finish()
	if err != nil || result != (Totals{CompletedCalls: 2, RejectedCalls: 1, LogFailures: 1,
		DurationNanos: 13, MaximumNanos: 9, FinalizedLogBytes: 129, MaximumLogBytes: 129}) {
		t.Fatalf("unexpected aggregate: %+v, %v", result, err)
	}
}

func TestUnassociatedLogsCannotBecomeMeasuredZero(t *testing.T) {
	a := New()
	feed(t, a, Event{Kind: LogFinalized, Time: 1, TID: 1, LogSizeBytes: 123},
		Event{Kind: CheckEnter, Time: 2, TID: 1})
	if err := a.Observe(Event{Kind: CheckReturn, Time: 3, TID: 1}); err == nil {
		t.Fatal("missing verifier log observation became a complete call")
	}
	if _, err := a.Finish(); err == nil {
		t.Fatal("incomplete call qualified")
	}
}

func TestMalformedOrderingAndIncompleteCallsRemainFailures(t *testing.T) {
	for _, events := range [][]Event{
		{{Kind: CheckReturn, Time: 1, TID: 1}},
		{{Kind: CheckEnter, Time: 1, TID: 1}, {Kind: CheckEnter, Time: 2, TID: 1}},
		{{Kind: CheckEnter, Time: 2, TID: 1}, {Kind: LogFinalized, Time: 1, TID: 1}},
		{{Kind: CheckEnter, Time: 1, TID: 1}, {Kind: LogFinalized, Time: 1, TID: 1}},
		{{Kind: CheckEnter, Time: 1, TID: 1}, {Kind: LogFinalized, Time: maxDuration + 2, TID: 1}},
		{{Kind: CheckEnter, Time: 1, TID: 1}, {Kind: LogFinalized, Time: 2, TID: 1}, {Kind: LogFinalized, Time: 3, TID: 1}},
		{{Kind: CheckEnter, Time: 1, TID: 0}},
		{{Kind: CheckEnter, Time: 1, TID: 1, Result: 1}},
		{{Kind: CheckEnter, Time: 1, TID: 1, LogSizeBytes: 1}},
		{{Kind: 99, Time: 1, TID: 1}},
	} {
		a := New()
		var err error
		for _, event := range events {
			err = a.Observe(event)
		}
		if err == nil {
			t.Fatal("invalid event sequence accepted")
		}
		if a.Observe(Event{Kind: CheckEnter, Time: maxDuration + 3, TID: 2}) == nil {
			t.Fatal("later valid event cleared failure")
		}
	}
	a := New()
	feed(t, a, Event{Kind: CheckEnter, Time: 1, TID: 1})
	if snapshot, err := a.Snapshot(); err != nil || snapshot.PendingCalls != 1 {
		t.Fatal("pending call hidden")
	}
	if _, err := a.Finish(); err == nil {
		t.Fatal("pending call qualified at end")
	}
}

func TestBoundsAndTransientIdentityRedaction(t *testing.T) {
	a := New()
	for tid := uint32(1); tid <= maxConcurrent; tid++ {
		feed(t, a, Event{Kind: CheckEnter, Time: uint64(tid), TID: tid})
	}
	if a.Observe(Event{Kind: CheckEnter, Time: 100, TID: 100}) == nil {
		t.Fatal("pending map grew beyond bound")
	}
	event := Event{Kind: CheckEnter, Time: 123, TID: 456}
	if _, err := json.Marshal(event); err == nil {
		t.Fatal("transient identity became serializable")
	}
	if fmt.Sprintf("%+v", event) != "[private verifier event]" {
		t.Fatal("transient identity leaked")
	}
}

func TestCompletedCallAndEventLimitsDoNotWrapOrTruncate(t *testing.T) {
	a := New()
	for n := uint64(0); n < maxCalls; n++ {
		feed(t, a, Event{Kind: CheckEnter, Time: n*3 + 1, TID: 1},
			Event{Kind: LogFinalized, Time: n*3 + 2, TID: 1, LogSizeBytes: ^uint32(0)},
			Event{Kind: CheckReturn, Time: n*3 + 3, TID: 1})
	}
	result, err := a.Snapshot()
	if err != nil || result.CompletedCalls != maxCalls || result.FinalizedLogBytes != maxCalls*uint64(^uint32(0)) {
		t.Fatalf("bounded counters changed: %+v, %v", result, err)
	}
	if a.Observe(Event{Kind: CheckEnter, Time: maxCalls*3 + 1, TID: 1}) == nil {
		t.Fatal("excess verifier call was accepted")
	}
	a = New()
	for n := uint64(1); n <= maxEvents; n++ {
		feed(t, a, Event{Kind: LogFinalized, Time: n, TID: 1})
	}
	if a.Observe(Event{Kind: LogFinalized, Time: maxEvents + 1, TID: 1}) == nil {
		t.Fatal("excess unrelated log events were silently dropped")
	}
}

func TestFinishedAggregateCannotAcceptMoreEvents(t *testing.T) {
	a := New()
	if result, err := a.Finish(); err != nil || result != (Totals{}) {
		t.Fatal("empty control observation changed")
	}
	if a.Observe(Event{Kind: CheckEnter, Time: 1, TID: 1}) == nil {
		t.Fatal("final snapshot accepted more events")
	}
}

func TestWatermarkBoundsSilentPendingCallsAndRejectsLateEvents(t *testing.T) {
	a := New()
	feed(t, a, Event{Kind: CheckEnter, Time: 1, TID: 1})
	if a.Advance(maxDuration+1) != nil {
		t.Fatal("maximum permitted duration changed")
	}
	if a.Advance(maxDuration+2) == nil {
		t.Fatal("silent missing return exceeded its bound")
	}
	if _, err := a.Snapshot(); err == nil {
		t.Fatal("watermark failure became a valid snapshot")
	}
	a = New()
	if a.Advance(100) != nil {
		t.Fatal("empty control watermark failed")
	}
	if a.Observe(Event{Kind: CheckEnter, Time: 99, TID: 1}) == nil {
		t.Fatal("late event rewrote the committed interval")
	}
}
