package traceframe

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceevidence"
)

// ClientClaim contains only identities visible to the authorised requester.
// The controller owns verification of the private Node UID and cgroup binding.
type ClientClaim struct {
	SessionID, EngineDigest, ProgrammeDigest string
	Namespace, Pod, PodUID, Container        string
	ContainerStartedAt                       time.Time
	Kind                                     trace.Kind
	Paths                                    trace.PathPolicy
	Bounds                                   trace.Bounds
}

type ClientMetadata struct {
	ClientClaim
	BindingDigest              string
	SessionStartedAt, Deadline time.Time
}

func (ClientClaim) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "[private trace claim]") }
func (ClientClaim) MarshalJSON() ([]byte, error) { return nil, ErrInvalid }
func (ClientMetadata) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[private trace metadata]")
}
func (ClientMetadata) MarshalJSON() ([]byte, error) { return nil, ErrInvalid }

// ClientMetadata projects a validated metadata frame without inventing private
// runtime identifiers. The binding digest remains an opaque server commitment.
func (f Frame) ClientMetadata() (ClientMetadata, error) {
	if f.kind != MetadataFrame {
		return ClientMetadata{}, ErrInvalid
	}
	var e envelope
	if json.Unmarshal([]byte(f.data), &e) != nil || e.Metadata == nil {
		return ClientMetadata{}, ErrInvalid
	}
	m := e.Metadata
	return ClientMetadata{ClientClaim: ClientClaim{SessionID: m.SessionID, EngineDigest: m.EngineDigest, ProgrammeDigest: m.ProgrammeDigest,
		Namespace: m.Target.Namespace, Pod: m.Target.Pod, PodUID: m.Target.PodUID, Container: m.Target.Container, ContainerStartedAt: m.Target.ContainerStartedAt,
		Kind: m.Kind, Paths: m.Paths, Bounds: m.bounds()}, BindingDigest: m.Target.BindingDigest, SessionStartedAt: m.SessionStartedAt, Deadline: m.Deadline}, nil
}

// MatchClientClaim checks the preflight-reviewed intent and selected lifetime.
// Pending admission expiry is deliberately not the active stream deadline.
func (f Frame) MatchClientClaim(expected ClientClaim) (ClientMetadata, error) {
	m, err := f.ClientMetadata()
	if err != nil {
		return ClientMetadata{}, err
	}
	received := m.ClientClaim
	received.ContainerStartedAt = received.ContainerStartedAt.UTC()
	expected.ContainerStartedAt = expected.ContainerStartedAt.UTC()
	if received != expected {
		return ClientMetadata{}, ErrInvalid
	}
	return m, nil
}

// ClientSummary returns bounded numeric evidence and caveats. It never exposes
// file paths, victim identifiers or event payloads. Reader must validate order
// and cumulative limits before this projection is used as a terminal result.
func (f Frame) ClientSummary() (Summary, error) {
	if f.kind != SummaryFrame {
		return Summary{}, ErrInvalid
	}
	var e envelope
	if json.Unmarshal([]byte(f.data), &e) != nil || e.Summary == nil {
		return Summary{}, ErrInvalid
	}
	s := e.Summary
	var start, end time.Time
	if s.ObservationStartedAt != nil && s.ObservationEndedAt != nil {
		start, end = *s.ObservationStartedAt, *s.ObservationEndedAt
	}
	correlation, err := traceevidence.Decode(s.Correlation, start, end, 5*time.Minute)
	if err != nil {
		return Summary{}, err
	}
	oom, err := traceevidence.OOMDecode(s.OOMCorrelation, start, end, 5*time.Minute)
	if err != nil {
		return Summary{}, err
	}
	kubernetes, err := traceevidence.KubernetesOOMDecode(s.KubernetesContext, 5*time.Minute)
	if err != nil {
		return Summary{}, err
	}
	return Summary{SessionEndedAt: s.SessionEndedAt, ObservationStartedAt: s.ObservationStartedAt, ObservationEndedAt: s.ObservationEndedAt,
		Termination: s.Termination, EngineCounts: trace.Counts{Produced: s.EngineCounts.Produced, Sampled: s.EngineCounts.Sampled, Lost: s.EngineCounts.Lost, Rejected: s.EngineCounts.Rejected},
		WrittenEvents: s.WrittenEvents, RejectedEvents: s.RejectedEvents, WrittenBytesBeforeSummary: s.WrittenBytesBeforeSummary, Incomplete: s.Incomplete,
		Aggregates: domainAggregates(*s), Correlation: correlation, OOMCorrelation: oom, KubernetesContext: kubernetes}, nil
}
