package delivery

import (
	"slices"
	"time"
)

type Latency struct {
	Events                       int     `json:"receivedEvents"`
	ProducedEvents               uint64  `json:"producedEvents"`
	UndeliveredEvents            uint64  `json:"undeliveredEvents"`
	EventLossPercent             float64 `json:"eventLossPercent"`
	LossBudgetPassed             bool    `json:"normalLossBudgetPassed"`
	FirstEventAfterReadNanos     int64   `json:"firstEventAfterReadNanos"`
	FirstEventDeliveryUpperNanos int64   `json:"firstEventDeliveryUpperNanos"`
	P50UpperNanos                *int64  `json:"p50UpperNanos"`
	P95UpperNanos                *int64  `json:"p95UpperNanos"`
	P99UpperNanos                *int64  `json:"p99UpperNanos"`
	MaximumDeliveredUpperNanos   int64   `json:"maximumDeliveredUpperNanos"`
	Passed                       bool    `json:"eventDeliveryBudgetPassed"`
}

// Latencies requires externally verified same-clock provenance. Uncertainty is
// measured, never assumed zero. Missing events sort after every delivered event;
// an unbounded percentile is null. A complete over-budget run remains a failure,
// not incomplete transport to discard and retry.
func Latencies(result Result) (Latency, error) {
	var out Latency
	if result.SchemaVersion != 1 || result.Scope != scope || result.Frames != len(result.Timings)+2 || result.EncodedBytes == 0 || result.ClientClockDriftNanos < 0 || result.ClientClockDriftNanos > int64(5*time.Millisecond) || !validCounts(result.Counts, len(result.Timings)) || !result.TransportComplete || !result.MetadataMatched || !result.HookCoverageIncomplete || result.AlignmentUncertaintyNanos == nil || *result.AlignmentUncertaintyNanos < 0 || *result.AlignmentUncertaintyNanos > int64(5*time.Millisecond) || len(result.Timings) == 0 || len(result.Timings) > 10000 {
		return out, ErrObservation
	}
	uncertainty := *result.AlignmentUncertaintyNanos + result.ClientClockDriftNanos
	values := make([]int64, 0, len(result.Timings))
	for index, timing := range result.Timings {
		if index > 0 && timing.ElapsedSinceReadNanos < result.Timings[index-1].ElapsedSinceReadNanos {
			return out, ErrObservation
		}
		if timing.ObservedNanos <= 0 || timing.ReceivedNanos <= 0 || timing.ElapsedSinceReadNanos < 0 || timing.ElapsedSinceReadNanos > int64(40*time.Second) {
			return out, ErrObservation
		}
		delta := timing.ReceivedNanos - timing.ObservedNanos
		if delta < -uncertainty || delta > int64(40*time.Second) {
			return out, ErrObservation
		}
		values = append(values, delta+uncertainty)
	}
	out.Events = len(values)
	out.ProducedEvents = *result.Counts.Produced
	out.UndeliveredEvents = out.ProducedEvents - uint64(out.Events)
	out.EventLossPercent = float64(out.UndeliveredEvents) * 100 / float64(out.ProducedEvents)
	// Strictly below 0.1%, with no rounding or multiplication overflow. Normal
	// runs do not declare sampling or malformed-record rejection as acceptable.
	out.LossBudgetPassed = out.UndeliveredEvents <= (out.ProducedEvents-1)/1000 && *result.Counts.Sampled == 0 && *result.Counts.Rejected == 0
	out.FirstEventAfterReadNanos = result.Timings[0].ElapsedSinceReadNanos
	out.FirstEventDeliveryUpperNanos = values[0]
	slices.Sort(values)
	rank := func(percent uint64) *int64 {
		index := out.ProducedEvents/100*percent + (out.ProducedEvents%100*percent+99)/100
		if index == 0 || index > uint64(len(values)) {
			return nil
		}
		return &values[index-1]
	}
	out.P50UpperNanos, out.P95UpperNanos, out.P99UpperNanos, out.MaximumDeliveredUpperNanos = rank(50), rank(95), rank(99), values[len(values)-1]
	out.Passed = out.LossBudgetPassed && out.P95UpperNanos != nil && *out.P95UpperNanos < int64(250*time.Millisecond) && out.P99UpperNanos != nil && *out.P99UpperNanos < int64(time.Second)
	return out, nil
}
