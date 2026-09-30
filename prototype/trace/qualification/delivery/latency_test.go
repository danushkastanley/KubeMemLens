package delivery

import (
	"testing"
	"time"
)

func timingResult(delay time.Duration) Result {
	uncertainty := int64(1000)
	one, zero := uint64(1), uint64(0)
	return Result{SchemaVersion: 1, Scope: scope, MetadataMatched: true, TransportComplete: true, HookCoverageIncomplete: true, Frames: 3, EncodedBytes: 1000, AlignmentUncertaintyNanos: &uncertainty, Counts: &Counts{&one, &zero, &zero, &zero}, Timings: []Timing{{1000000000, 1000000000 + int64(delay), 2000000}}}
}
func TestExactLatencyCeilingAndUnknownCountersFail(t *testing.T) {
	for _, test := range []struct {
		delay  time.Duration
		passed bool
	}{{250*time.Millisecond - time.Microsecond - time.Nanosecond, true}, {250*time.Millisecond - time.Microsecond, false}, {time.Second, false}} {
		value, err := Latencies(timingResult(test.delay))
		if err != nil || value.Passed != test.passed {
			t.Fatal(value, err)
		}
	}
	for _, change := range []string{"unknown-counts", "unknown-alignment", "clock-drift", "future-event", "incomplete", "coverage-overclaim", "no-events"} {
		result := timingResult(time.Millisecond)
		switch change {
		case "unknown-counts":
			result.Counts = nil
		case "unknown-alignment":
			result.AlignmentUncertaintyNanos = nil
		case "clock-drift":
			result.ClientClockDriftNanos = int64(6 * time.Millisecond)
		case "future-event":
			result.Timings[0].ObservedNanos += int64(time.Second)
		case "incomplete":
			result.TransportComplete = false
		case "coverage-overclaim":
			result.HookCoverageIncomplete = false
		case "no-events":
			result.Timings = nil
		}
		if _, err := Latencies(result); err == nil {
			t.Fatal("invalid timing result accepted", change)
		}
	}
}
func TestAlignmentUncertaintyIsConservativeRatherThanDiscarded(t *testing.T) {
	result := timingResult(-500 * time.Nanosecond)
	value, err := Latencies(result)
	if err != nil || *value.P99UpperNanos != 500 {
		t.Fatal(value, err)
	}
	result.Timings[0].ObservedNanos += 10000
	if _, err := Latencies(result); err == nil {
		t.Fatal("clock contradiction accepted")
	}
}

func TestP99BoundAppliesEvenWhenP95IsFast(t *testing.T) {
	result := timingResult(time.Millisecond)
	hundred := uint64(100)
	result.Counts.Produced = &hundred
	result.Frames = 102
	original := result.Timings[0]
	result.Timings = make([]Timing, 100)
	for i := range result.Timings {
		result.Timings[i] = original
	}
	for _, i := range []int{98, 99} {
		result.Timings[i].ReceivedNanos = result.Timings[i].ObservedNanos + int64(time.Second) - *result.AlignmentUncertaintyNanos
	}
	value, err := Latencies(result)
	if err != nil || value.Passed || *value.P95UpperNanos >= int64(250*time.Millisecond) || *value.P99UpperNanos != int64(time.Second) {
		t.Fatal(value, err)
	}
	for _, i := range []int{98, 99} {
		result.Timings[i].ReceivedNanos--
	}
	value, err = Latencies(result)
	if err != nil || !value.Passed {
		t.Fatal(value, err)
	}
}

func TestLossThresholdIsStrictAndMissingEventsCannotImprovePercentiles(t *testing.T) {
	for _, test := range []struct {
		produced uint64
		received int
		passed   bool
	}{{1000, 999, false}, {1001, 1000, true}, {100, 1, false}} {
		result := timingResult(time.Millisecond)
		sample := result.Timings[0]
		result.Timings = make([]Timing, test.received)
		for i := range result.Timings {
			result.Timings[i] = sample
		}
		lost := test.produced - uint64(test.received)
		result.Counts.Produced = &test.produced
		result.Counts.Lost = &lost
		result.Frames = test.received + 2
		value, err := Latencies(result)
		if err != nil || value.Passed != test.passed {
			t.Fatal(value, err)
		}
		if test.received == 1 && (value.P50UpperNanos != nil || value.P95UpperNanos != nil || value.P99UpperNanos != nil) {
			t.Fatal("missing events became a fast percentile")
		}
	}
}
