package traceframe

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/bits"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceevidence"
)

func NewMetadata(m Metadata) (Frame, error) {
	return NewMetadataVersion(m, Version)
}

func NewMetadataVersion(m Metadata, version int) (Frame, error) {
	if !SupportedVersion(version) || m.Specification.Validate() != nil || (version == AggregateVersion && m.Specification.Kind() == trace.OOM) {
		return Frame{}, ErrInvalid
	}
	s, t, b := m.Specification, m.Specification.Target(), m.Specification.Bounds()
	// Only this explicit ephemeral encoder may serialise the private identity.
	// The committed representation includes every immutable target field.
	identity := struct {
		Namespace, Pod, PodUID, Container, ContainerID, NodeUID string
		StartedAt                                               time.Time
		CgroupID                                                uint64
	}{t.Namespace, t.PodName, t.PodUID, t.ContainerName, t.ContainerID, t.NodeUID, t.ContainerStartedAt.UTC(), t.CgroupID}
	data, err := json.Marshal(identity)
	if err != nil {
		return Frame{}, ErrInvalid
	}
	digest := sha256.Sum256(data)
	metadata := wireMetadata{
		SessionID: m.SessionID, EngineDigest: m.EngineDigest, ProgrammeDigest: m.ProgrammeDigest, Kind: s.Kind(),
		Target: wireTarget{t.Namespace, t.PodName, t.PodUID, t.ContainerName, t.ContainerStartedAt.UTC(), "sha256:" + hex.EncodeToString(digest[:])},
		Paths:  s.Paths(), Bounds: wireBounds{int64(b.Duration), b.Events, b.OutputBytes, b.MapBytes, b.PathBytes}, SessionStartedAt: m.SessionStartedAt.UTC(), Deadline: m.Deadline.UTC(),
	}
	if err := validateMetadata(metadata); err != nil {
		return Frame{}, err
	}
	return makeFrame(envelope{Version: version, Type: MetadataFrame, Metadata: &metadata})
}

func NewSummary(s Summary) (Frame, error) {
	return NewSummaryVersion(s, Version)
}

func NewSummaryVersion(s Summary, version int) (Frame, error) {
	if !SupportedVersion(version) || (version == Version && (s.Aggregates != nil || s.Correlation != nil)) || (version == AggregateVersion && !s.Incomplete) {
		return Frame{}, ErrInvalid
	}
	if s.SessionEndedAt.IsZero() || !validTermination(s.Termination) || s.WrittenEvents > 100000 || s.WrittenBytesBeforeSummary > 32<<20 {
		return Frame{}, ErrInvalid
	}
	if (s.ObservationStartedAt == nil) != (s.ObservationEndedAt == nil) {
		return Frame{}, ErrInvalid
	}
	if s.ObservationStartedAt != nil && (s.ObservationStartedAt.IsZero() || s.ObservationEndedAt.Before(*s.ObservationStartedAt) || s.ObservationEndedAt.After(s.SessionEndedAt)) {
		return Frame{}, ErrInvalid
	}
	counts := s.EngineCounts
	if !s.Incomplete && (s.Termination != trace.Expired || s.ObservationStartedAt == nil || counts.Produced == nil || counts.Sampled == nil || counts.Lost == nil || counts.Rejected == nil || *counts.Sampled != 0 || *counts.Lost != 0 || *counts.Rejected != 0 || s.RejectedEvents != 0) {
		return Frame{}, ErrInvalid
	}
	if !s.Incomplete {
		total, carry := bits.Add64(s.WrittenEvents, s.RejectedEvents, 0)
		overflow := carry != 0
		for _, count := range []uint64{*counts.Sampled, *counts.Lost, *counts.Rejected} {
			total, carry = bits.Add64(total, count, 0)
			overflow = overflow || carry != 0
		}
		if overflow || total != *counts.Produced {
			return Frame{}, ErrInvalid
		}
	}
	wire := wireSummary{SessionEndedAt: s.SessionEndedAt.UTC(), ObservationStartedAt: s.ObservationStartedAt, ObservationEndedAt: s.ObservationEndedAt, Termination: s.Termination, EngineCounts: wireCounts{counts.Produced, counts.Sampled, counts.Lost, counts.Rejected}, WrittenEvents: s.WrittenEvents, RejectedEvents: s.RejectedEvents, WrittenBytesBeforeSummary: s.WrittenBytesBeforeSummary, Incomplete: s.Incomplete}
	if version == AggregateVersion {
		var start, end time.Time
		if s.ObservationStartedAt != nil {
			start, end = *s.ObservationStartedAt, *s.ObservationEndedAt
		}
		correlation, err := traceevidence.Encode(s.Correlation, start, end, 5*time.Minute)
		if err != nil || (s.Correlation != nil && s.Correlation.EvidenceEnd.After(s.SessionEndedAt)) {
			return Frame{}, ErrInvalid
		}
		wire.Correlation = correlation
		if err := setAggregates(&wire, s.Aggregates); err != nil {
			return Frame{}, err
		}
	}
	frame, err := makeFrame(envelope{Version: version, Type: SummaryFrame, Summary: &wire})
	if len(frame.data) > Reserve(version) {
		return Frame{}, ErrInvalid
	}
	return frame, err
}

func (m wireMetadata) bounds() trace.Bounds {
	return trace.Bounds{Duration: time.Duration(m.Bounds.DurationNanos), Events: m.Bounds.Events, OutputBytes: m.Bounds.OutputBytes, MapBytes: m.Bounds.MapBytes, PathBytes: m.Bounds.PathBytes}
}
