package tracereport

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"reflect"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceaggregate"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

func readSummary(data []byte, observed observedWire) (traceframe.Summary, uint64, error) {
	var w summaryWire
	if jsonv2.Unmarshal(data, &w, jsonv2.RejectUnknownMembers(true)) != nil {
		return traceframe.Summary{}, 0, ErrInvalid
	}
	s := traceframe.Summary{SessionEndedAt: w.SessionEndedAt, ObservationStartedAt: w.ObservationStartedAt, ObservationEndedAt: w.ObservationEndedAt,
		Termination: w.Termination, Incomplete: w.Incomplete, EngineCounts: trace.Counts{Produced: w.EngineCounts.Produced, Sampled: w.EngineCounts.Sampled, Lost: w.EngineCounts.Lost, Rejected: w.EngineCounts.Rejected},
		WrittenEvents: w.WrittenEvents, RejectedEvents: w.RejectedEvents, WrittenBytesBeforeSummary: w.WrittenBytesBeforeSummary}
	var err error
	if w.Aggregates != nil {
		s.Aggregates, err = w.Aggregates.domain()
		if err != nil {
			return s, 0, err
		}
	}
	if w.Correlation != nil {
		s.Correlation = w.Correlation.domain()
	}
	if w.OOMCorrelation != nil {
		s.OOMCorrelation = w.OOMCorrelation.domain()
	}
	if w.KubernetesContext != nil {
		s.KubernetesContext = w.KubernetesContext.domain()
	}
	frame, err := traceframe.NewSummaryVersion(s, observed.StreamVersion)
	if err != nil || !sameProjection(data, summaryFields(s)) {
		return s, 0, ErrInvalid
	}
	encoded, err := traceframe.Encode(frame)
	if err != nil || len(encoded) > traceframe.Reserve(observed.StreamVersion) {
		return s, 0, ErrInvalid
	}
	if s.SessionEndedAt.Before(observed.SessionStartedAt) || (s.Termination == trace.Expired && s.SessionEndedAt.Before(observed.Deadline)) {
		return s, 0, ErrInvalid
	}
	var start, end time.Time
	if s.ObservationStartedAt != nil {
		start, end = *s.ObservationStartedAt, *s.ObservationEndedAt
		if start.Before(observed.SessionStartedAt) || end.After(observed.Deadline) {
			return s, 0, ErrInvalid
		}
	}
	duration := observed.Bounds.domain().Duration
	if s.Correlation != nil && (s.Correlation.Validate(start, end, duration) != nil || (s.Correlation.State == "overlapping" && s.Correlation.EvidenceStart.Before(observed.SessionStartedAt))) {
		return s, 0, ErrInvalid
	}
	if s.OOMCorrelation != nil && (s.OOMCorrelation.Validate(start, end, duration) != nil || (s.OOMCorrelation.Window.State == "overlapping" && s.OOMCorrelation.Window.EvidenceStart.Before(observed.SessionStartedAt))) {
		return s, 0, ErrInvalid
	}
	if s.KubernetesContext != nil && s.KubernetesContext.Validate(duration) != nil {
		return s, 0, ErrInvalid
	}
	if a := s.Aggregates; a != nil {
		if a.Kind != observed.Kind || a.Observations > observed.Bounds.Events || (observed.Paths == trace.ConfirmedPaths && a.Observations != s.WrittenEvents) {
			return s, 0, ErrInvalid
		}
	}
	return s, uint64(len(encoded)), nil
}

// Reprojection checks required fields and null/zero distinctions that ordinary
// Go zero values cannot express. Numbers retain exact decimal text and precision.
func sameProjection(data []byte, projection any) bool {
	encoded, err := json.Marshal(projection)
	if err != nil {
		return false
	}
	decode := func(data []byte) (any, error) {
		var value any
		d := json.NewDecoder(bytes.NewReader(data))
		d.UseNumber()
		err := d.Decode(&value)
		return value, err
	}
	original, err := decode(data)
	if err != nil {
		return false
	}
	expected, err := decode(encoded)
	return err == nil && reflect.DeepEqual(original, expected)
}
func (t totalWire) domain() traceaggregate.Total {
	return traceaggregate.Total{Value: t.Value, Unreported: t.Unreported, Overflow: t.Overflow}
}
func (f fileOperationsWire) domain() traceaggregate.FileOperations {
	return traceaggregate.FileOperations{Operations: f.Operations, RequestedBytes: f.RequestedBytes.domain(), CompletedBytes: f.CompletedBytes.domain()}
}
func (c cacheOperationsWire) domain() traceaggregate.CacheOperations {
	return traceaggregate.CacheOperations{Operations: c.Operations, Pages: c.Pages.domain()}
}
func (a aggregateWire) domain() (*traceaggregate.Summary, error) {
	out := &traceaggregate.Summary{Kind: a.Kind, Observations: a.Observations}
	switch a.Kind {
	case trace.Files:
		if a.Reads == nil || a.Writes == nil || a.Additions != nil || a.Removals != nil || a.Decisions != nil {
			return nil, ErrInvalid
		}
		out.Reads, out.Writes = a.Reads.domain(), a.Writes.domain()
	case trace.Cache:
		if a.Additions == nil || a.Removals == nil || a.Reads != nil || a.Writes != nil || a.Decisions != nil {
			return nil, ErrInvalid
		}
		out.Additions, out.Removals = a.Additions.domain(), a.Removals.domain()
	case trace.OOM:
		if a.Decisions == nil || a.Reads != nil || a.Writes != nil || a.Additions != nil || a.Removals != nil {
			return nil, ErrInvalid
		}
		out.OOM = traceaggregate.OOMCounts{Cgroup: a.Decisions.Cgroup, Global: a.Decisions.Global, Unknown: a.Decisions.Unknown, MissingProcessContext: a.Decisions.MissingProcessContext}
	default:
		return nil, ErrInvalid
	}
	return out, nil
}
func (g gaugeWire) domain() trace.GaugePair { return trace.GaugePair{Before: g.Before, After: g.After} }
func (d deltaWire) domain() trace.CounterDelta {
	return trace.CounterDelta{State: d.State, Delta: d.Delta}
}
func (w windowWire) domain() trace.CorrelationWindow {
	return trace.CorrelationWindow{State: w.State, EvidenceStart: w.EvidenceStart, BeforeEnd: w.BeforeEnd, AfterStart: w.AfterStart, EvidenceEnd: w.EvidenceEnd, OverlapStart: w.OverlapStart, OverlapEnd: w.OverlapEnd, Uncertainty: (*time.Duration)(w.UncertaintyNanos)}
}
func (c correlationWire) domain() *trace.Correlation {
	w := c.windowWire.domain()
	return &trace.Correlation{State: w.State, EvidenceStart: w.EvidenceStart, BeforeEnd: w.BeforeEnd, AfterStart: w.AfterStart, EvidenceEnd: w.EvidenceEnd, OverlapStart: w.OverlapStart, OverlapEnd: w.OverlapEnd, Uncertainty: w.Uncertainty,
		File: c.FileBytes.domain(), Dirty: c.DirtyBytes.domain(), Writeback: c.WritebackBytes.domain(), Refault: c.Refault.domain(), Scan: c.Scan.domain(), Steal: c.Steal.domain()}
}
func (e eventsWire) domain() trace.OOMEventDeltas {
	return trace.OOMEventDeltas{Low: e.Low.domain(), High: e.High.domain(), Max: e.Max.domain(), OOM: e.OOM.domain(), Kill: e.OOMKill.domain(), GroupKill: e.OOMGroupKill.domain()}
}
func (c oomCorrelationWire) domain() *trace.OOMCorrelation {
	return &trace.OOMCorrelation{Window: c.Window.domain(), Local: c.Local.domain(), Hierarchical: c.Hierarchical.domain(), Current: c.CurrentBytes.domain(), LimitBefore: trace.OOMLimit{State: c.LimitBefore.State, Bytes: c.LimitBefore.Bytes}, LimitAfter: trace.OOMLimit{State: c.LimitAfter.State, Bytes: c.LimitAfter.Bytes}, PSISome: c.SomeStallMicros.domain(), PSIFull: c.FullStallMicros.domain()}
}
func (k kubernetesWire) domain() *trace.KubernetesOOMContext {
	return &trace.KubernetesOOMContext{State: k.State, BeforeStart: k.BeforeStart, BeforeEnd: k.BeforeEnd, AfterStart: k.AfterStart, AfterEnd: k.AfterEnd, Restarts: k.Restarts.domain(), PressureBefore: k.PressureBefore, PressureAfter: k.PressureAfter}
}
