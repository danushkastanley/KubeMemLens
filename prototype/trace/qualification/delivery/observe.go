// Package delivery records numeric event timings at an authorised ephemeral
// stream boundary. The caller must establish a shared Linux clock and deadlines.
package delivery

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

var ErrObservation = errors.New("delivery observation incomplete or inconsistent")

const scope = "same-clock confirmed-file-event observation"

const fixturePath = "/work/fixed-seed.bin"

type Expectation struct {
	SessionID, EngineDigest, ProgrammeDigest string
	Specification                            trace.Specification
}

func (Expectation) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[private delivery expectation]")
}

type Timing struct {
	ObservedNanos         int64 `json:"observedNanos"`
	ReceivedNanos         int64 `json:"receivedNanos"`
	ElapsedSinceReadNanos int64 `json:"elapsedSinceReadNanos"`
}
type Result struct {
	SchemaVersion             int                `json:"schemaVersion"`
	Scope                     string             `json:"scope"`
	MetadataMatched           bool               `json:"metadataMatched"`
	TransportComplete         bool               `json:"transportComplete"`
	HookCoverageIncomplete    bool               `json:"hookCoverageIncomplete"`
	ClientClockDriftNanos     int64              `json:"clientClockDriftNanos"`
	Frames                    int                `json:"frames"`
	EncodedBytes              uint64             `json:"encodedBytes"`
	Timings                   []Timing           `json:"timings"`
	AlignmentUncertaintyNanos *int64             `json:"alignmentUncertaintyNanos,omitempty"`
	Counts                    *Counts            `json:"counts,omitempty"`
	SummaryDiagnostic         *SummaryDiagnostic `json:"summaryDiagnostic,omitempty"`
}

// SummaryDiagnostic retains only validated enum and numeric fields. It explains
// rejected terminal evidence without retaining the raw frame or any identity.
// Its presence does not establish complete transport or measurable latency.
type SummaryDiagnostic struct {
	Termination            string `json:"termination"`
	CorrelationState       string `json:"correlationState"`
	UncertaintyNanos       *int64 `json:"uncertaintyNanos"`
	EndedAfterReceiptNanos int64  `json:"endedAfterReceiptNanos"`
	Counts                 Counts `json:"counts"`
}
type Counts struct {
	Produced *uint64 `json:"produced"`
	Sampled  *uint64 `json:"sampled"`
	Lost     *uint64 `json:"lost"`
	Rejected *uint64 `json:"rejected"`
}

type wireSubset struct {
	Metadata *struct {
		Deadline time.Time `json:"deadline"`
	} `json:"metadata"`
	Event *struct {
		ObservedAt time.Time `json:"observedAt"`
		File       *struct {
			Path *string `json:"path"`
		} `json:"file"`
	} `json:"event"`
	Summary *struct {
		Termination    string    `json:"termination"`
		SessionEndedAt time.Time `json:"sessionEndedAt"`
		Incomplete     bool      `json:"incomplete"`
		EngineCounts   Counts    `json:"engineCounts"`
		Correlation    *struct {
			State            string `json:"state"`
			UncertaintyNanos *int64 `json:"uncertaintyNanos"`
		} `json:"correlation"`
	} `json:"summary"`
}

// Observe uses the production validator before decoding a temporary in-memory
// subset. activeDeadline must read the ACTIVE admission, not the POST pending TTL.
// Timestamping after Next conservatively includes decoding/validation delay.
// No raw frame, path, credential or target is returned or written.
func Observe(input io.Reader, expected Expectation, now func() time.Time, activeDeadline func() (time.Time, error)) (Result, error) {
	result := Result{SchemaVersion: 1, Scope: scope, Timings: []Timing{}}
	spec := expected.Specification
	if !validExpectation(expected) || now == nil || activeDeadline == nil {
		return result, ErrObservation
	}
	reader := traceframe.NewReader(io.LimitReader(input, int64(spec.Bounds().OutputBytes)+1))
	started := now()
	for {
		frame, err := reader.Next()
		received := now()
		if err == io.EOF {
			break
		}
		if err != nil || frame.Version() != traceframe.AggregateVersion {
			return result, ErrObservation
		}
		if received.Before(started) || received.Sub(started) > 40*time.Second {
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
		var value wireSubset
		if json.Unmarshal(raw, &value) != nil {
			return result, ErrObservation
		}
		result.Frames++
		result.EncodedBytes += uint64(len(raw))
		switch frame.Type() {
		case traceframe.MetadataFrame:
			if value.Metadata == nil || !value.Metadata.Deadline.After(started) || value.Metadata.Deadline.After(started.Add(spec.Bounds().Duration+5*time.Second)) {
				return result, ErrObservation
			}
			deadline, err := activeDeadline()
			if err != nil || value.Metadata == nil || !value.Metadata.Deadline.Equal(deadline) || frame.MatchAdmissionVersion(expected.SessionID, expected.EngineDigest, expected.ProgrammeDigest, spec, deadline, traceframe.AggregateVersion) != nil {
				return result, ErrObservation
			}
			result.MetadataMatched = true
		case traceframe.EventFrame:
			if value.Event == nil || value.Event.File == nil || value.Event.File.Path == nil || len(result.Timings) >= int(spec.Bounds().Events) {
				return result, ErrObservation
			}
			path, err := traceframe.DecodeText(*value.Event.File.Path, spec.Bounds().PathBytes)
			if err != nil || path != fixturePath {
				return result, ErrObservation
			}
			result.Timings = append(result.Timings, Timing{value.Event.ObservedAt.UnixNano(), received.UnixNano(), received.Sub(started).Nanoseconds()})
		case traceframe.SummaryFrame:
			summary := value.Summary
			if summary != nil {
				result.SummaryDiagnostic = &SummaryDiagnostic{Termination: summary.Termination,
					CorrelationState: "absent", EndedAfterReceiptNanos: summary.SessionEndedAt.Sub(received).Nanoseconds(),
					Counts: summary.EngineCounts}
				if summary.Correlation != nil {
					result.SummaryDiagnostic.CorrelationState = summary.Correlation.State
					result.SummaryDiagnostic.UncertaintyNanos = summary.Correlation.UncertaintyNanos
				}
			}
			if summary == nil || summary.Termination != "expired" || !summary.Incomplete {
				return result, ErrObservation
			}
			result.HookCoverageIncomplete = summary.Incomplete
			if summary.Correlation == nil || summary.Correlation.State != "overlapping" || summary.Correlation.UncertaintyNanos == nil || *summary.Correlation.UncertaintyNanos < 0 || *summary.Correlation.UncertaintyNanos > int64(5*time.Millisecond) {
				return result, ErrObservation
			}
			if summary.SessionEndedAt.IsZero() || summary.SessionEndedAt.After(received.Add(time.Duration(*summary.Correlation.UncertaintyNanos))) {
				return result, ErrObservation
			}
			result.AlignmentUncertaintyNanos = summary.Correlation.UncertaintyNanos
			result.Counts = &summary.EngineCounts
		}
	}
	if !result.MetadataMatched || result.Counts == nil || len(result.Timings) == 0 {
		return result, ErrObservation
	}
	if !validCounts(result.Counts, len(result.Timings)) {
		return result, ErrObservation
	}
	result.TransportComplete = true
	return result, nil
}

func validCounts(counts *Counts, events int) bool {
	if counts == nil || counts.Produced == nil || counts.Sampled == nil || counts.Lost == nil || counts.Rejected == nil || *counts.Produced < uint64(events) {
		return false
	}
	missing, carry := bits.Add64(*counts.Sampled, *counts.Lost, 0)
	if carry != 0 {
		return false
	}
	missing, carry = bits.Add64(missing, *counts.Rejected, 0)
	return carry == 0 && *counts.Produced-uint64(events) == missing
}

func validExpectation(expected Expectation) bool {
	spec := expected.Specification
	if spec.Validate() != nil || spec.Kind() != trace.Files || spec.Paths() != trace.ConfirmedPaths || spec.Bounds().Duration > 30*time.Second || spec.Bounds().Events > 10000 || spec.Bounds().OutputBytes > 8<<20 || spec.Bounds().MapBytes > 8<<20 || spec.Bounds().PathBytes > 256 {
		return false
	}
	start := spec.Target().ContainerStartedAt.Add(time.Second)
	_, err := traceframe.NewMetadataVersion(traceframe.Metadata{SessionID: expected.SessionID, EngineDigest: expected.EngineDigest, ProgrammeDigest: expected.ProgrammeDigest, Specification: spec, SessionStartedAt: start, Deadline: start.Add(spec.Bounds().Duration)}, traceframe.AggregateVersion)
	return err == nil
}
