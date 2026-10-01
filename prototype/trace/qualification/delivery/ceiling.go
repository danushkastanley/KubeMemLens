package delivery

import (
	"encoding/json"
	"io"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

// CeilingResult records bounded transport and a reported ceiling only. It does
// not infer ring saturation, loss percentages, event latency or resource safety.
type CeilingResult struct {
	SchemaVersion          int     `json:"schemaVersion"`
	MetadataMatched        bool    `json:"metadataMatched"`
	TransportComplete      bool    `json:"transportComplete"`
	CeilingReported        bool    `json:"ceilingReported"`
	Frames                 uint64  `json:"frames"`
	Events                 uint64  `json:"events"`
	EncodedBytes           uint64  `json:"encodedBytes"`
	BytesBeforeSummary     uint64  `json:"bytesBeforeSummary"`
	Termination            string  `json:"termination"`
	HookCoverageIncomplete bool    `json:"hookCoverageIncomplete"`
	Counts                 *Counts `json:"counts,omitempty"`
	ClientClockDriftNanos  int64   `json:"clientClockDriftNanos"`
}

// ObserveCeiling is separate from normal latency observation. The caller must
// bound transport reads and verify a shared clock and the active admission.
// Unknown engine counters remain null; ordinary expiry is not a ceiling pass.
func ObserveCeiling(input io.Reader, expected Expectation, now func() time.Time, activeDeadline func() (time.Time, error)) (CeilingResult, error) {
	return observeCeiling(input, expected, now, activeDeadline, nil)
}

func observeCeiling(input io.Reader, expected Expectation, now func() time.Time, activeDeadline func() (time.Time, error), ready func() error) (CeilingResult, error) {
	result := CeilingResult{SchemaVersion: 1}
	if !validExpectation(expected) || now == nil || activeDeadline == nil {
		return result, ErrObservation
	}
	spec := expected.Specification
	reader := traceframe.NewReader(io.LimitReader(input, int64(spec.Bounds().OutputBytes)+1))
	started := now()
	for {
		frame, err := reader.Next()
		received := now()
		if err == io.EOF {
			break
		}
		if err != nil || frame.Version() != traceframe.AggregateVersion || received.Before(started) || received.Sub(started) > 40*time.Second {
			return result, ErrObservation
		}
		drift := received.UnixNano() - started.UnixNano() - received.Sub(started).Nanoseconds()
		if drift < 0 {
			drift = -drift
		}
		result.ClientClockDriftNanos = max(result.ClientClockDriftNanos, drift)
		if drift > int64(5*time.Millisecond) {
			return result, ErrObservation
		}
		raw, err := traceframe.Encode(frame)
		if err != nil {
			return result, ErrObservation
		}
		result.Frames++
		result.EncodedBytes += uint64(len(raw))
		switch frame.Type() {
		case traceframe.MetadataFrame:
			metadata, err := frame.ClientMetadata()
			if err != nil || !metadata.Deadline.After(started) || metadata.Deadline.After(started.Add(spec.Bounds().Duration+5*time.Second)) {
				return result, ErrObservation
			}
			deadline, err := activeDeadline()
			if err != nil || frame.MatchAdmissionVersion(expected.SessionID, expected.EngineDigest, expected.ProgrammeDigest, spec, deadline, traceframe.AggregateVersion) != nil {
				return result, ErrObservation
			}
			result.MetadataMatched = true
			if ready != nil {
				if err := ready(); err != nil {
					return result, ErrObservation
				}
			}
		case traceframe.EventFrame:
			var value wireSubset
			if json.Unmarshal(raw, &value) != nil || value.Event == nil || value.Event.File == nil || value.Event.File.Path == nil {
				return result, ErrObservation
			}
			path, err := traceframe.DecodeText(*value.Event.File.Path, spec.Bounds().PathBytes)
			if err != nil || path != fixturePath {
				return result, ErrObservation
			}
			result.Events++
		case traceframe.SummaryFrame:
			summary, err := frame.ClientSummary()
			if err != nil || !summary.Incomplete || summary.WrittenEvents != result.Events || summary.SessionEndedAt.After(received.Add(5*time.Millisecond)) {
				return result, ErrObservation
			}
			result.Termination = string(summary.Termination)
			result.HookCoverageIncomplete = summary.Incomplete
			result.BytesBeforeSummary = summary.WrittenBytesBeforeSummary
			result.Counts = &Counts{summary.EngineCounts.Produced, summary.EngineCounts.Sampled, summary.EngineCounts.Lost, summary.EngineCounts.Rejected}
			switch summary.Termination {
			case trace.EventLimit:
				result.CeilingReported = result.Events == spec.Bounds().Events
			case trace.OutputLimit:
				// The writer stops before an entire event plus terminal reserve
				// would cross the bound. It need not fill the final byte.
				used := result.BytesBeforeSummary + uint64(traceframe.AggregateTerminalReserve)
				result.CeilingReported = used <= spec.Bounds().OutputBytes && spec.Bounds().OutputBytes-used < uint64(traceframe.MaxBytes)
			}
		}
	}
	if !result.MetadataMatched || result.Counts == nil || result.Frames != result.Events+2 {
		return result, ErrObservation
	}
	result.TransportComplete = true
	if !result.CeilingReported {
		return result, ErrObservation
	}
	return result, nil
}
