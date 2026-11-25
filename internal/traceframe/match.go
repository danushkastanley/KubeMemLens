package traceframe

import (
	"encoding/json"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

// ClockSkewAllowance is how far a node's clock may be behind the controller's.
// A node that sees a controller instant further in its future than it can
// grant shortens it to its own clock by at most this much. See ADR 0029.
const ClockSkewAllowance = time.Second

// MatchAdmission checks authenticated node metadata against the controller's
// immutable claim and independently selected programme digest. The node may
// choose its session start, but cannot replace identity or policy. It cannot
// extend the deadline; it may shorten it by at most ClockSkewAllowance.
func (f Frame) MatchAdmission(id, engineDigest, programmeDigest string, spec trace.Specification, deadline time.Time) error {
	return f.MatchAdmissionVersion(id, engineDigest, programmeDigest, spec, deadline, Version)
}

func (f Frame) MatchAdmissionVersion(id, engineDigest, programmeDigest string, spec trace.Specification, deadline time.Time, version int) error {
	if !SupportedVersion(version) || f.kind != MetadataFrame || f.version != version {
		return ErrInvalid
	}
	var received envelope
	if json.Unmarshal([]byte(f.data), &received) != nil || received.Metadata == nil {
		return ErrInvalid
	}
	nodeDeadline := received.Metadata.Deadline
	if nodeDeadline.After(deadline) || nodeDeadline.Before(deadline.Add(-ClockSkewAllowance)) {
		return ErrInvalid
	}
	expected, err := NewMetadataVersion(Metadata{SessionID: id, EngineDigest: engineDigest, ProgrammeDigest: programmeDigest, Specification: spec, SessionStartedAt: received.Metadata.SessionStartedAt, Deadline: nodeDeadline}, version)
	if err != nil {
		return err
	}
	var reference envelope
	if json.Unmarshal([]byte(expected.data), &reference) != nil {
		return ErrInvalid
	}
	// Time is compared by instant, independent of a sender's equivalent timezone.
	received.Metadata.SessionStartedAt = received.Metadata.SessionStartedAt.UTC()
	received.Metadata.Deadline = received.Metadata.Deadline.UTC()
	received.Metadata.Target.ContainerStartedAt = received.Metadata.Target.ContainerStartedAt.UTC()
	if *received.Metadata != *reference.Metadata {
		return ErrInvalid
	}
	return nil
}
