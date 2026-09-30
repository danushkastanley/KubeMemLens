// Package tracereport creates explicit redacted, bounded operator exports.
// It never serialises raw frames or private runtime identities.
package tracereport

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceclient"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

const MaxBytes = 32 << 10

var ErrInvalid = errors.New("trace report is invalid or exceeds its byte limit")

// Document holds an immutable approved projection. No caller can insert raw
// frames or expand the exported field set by mutating its input afterwards.
type Document struct{ data string }

func (d Document) MarshalJSON() ([]byte, error) {
	if d.data == "" {
		return nil, ErrInvalid
	}
	return []byte(d.data), nil
}
func (d Document) Bytes() ([]byte, error) {
	if d.data == "" {
		return nil, ErrInvalid
	}
	return []byte(d.data + "\n"), nil
}

func New(snapshot traceclient.Snapshot, toolVersion string, capturedAt time.Time) (Document, error) {
	if !snapshot.State.Terminal() || capturedAt.IsZero() || len(toolVersion) == 0 || len(toolVersion) > 256 || !utf8.ValidString(toolVersion) || strings.ContainsAny(toolVersion, "\x00\r\n\x1b") {
		return Document{}, ErrInvalid
	}
	if snapshot.Cleanup != traceclient.CleanupConfirmed && snapshot.Cleanup != traceclient.CleanupUnconfirmed && snapshot.Cleanup != traceclient.CleanupNotRequested {
		return Document{}, ErrInvalid
	}
	if snapshot.Intent.Kind != "" && trace.ValidateIntent(snapshot.Intent.Kind, snapshot.Intent.Paths, snapshot.Intent.Bounds) != nil {
		return Document{}, ErrInvalid
	}
	m := snapshot.Result.Metadata
	if m.SessionID != "" && (!traceframe.AllowsKind(snapshot.Result.StreamVersion, m.Kind) || trace.ValidateIntent(m.Kind, m.Paths, m.Bounds) != nil || !reportDigest(m.EngineDigest) || !reportDigest(m.ProgrammeDigest) || m.SessionStartedAt.IsZero() || !m.Deadline.After(m.SessionStartedAt)) {
		return Document{}, ErrInvalid
	}
	caveats := []string{"Target aliases apply only within this report.", "Raw events, paths, process details and runtime identifiers are omitted.", "Transport completion, observation coverage and cleanup confirmation are separate.", "This report does not establish resource or provider qualification."}
	doc := map[string]any{"schemaVersion": 1, "kind": "TraceReport", "capturedAt": capturedAt.UTC(), "toolVersion": toolVersion, "redacted": true,
		"state": snapshot.State, "cleanup": snapshot.Cleanup, "failure": errorCode(snapshot.Failure), "cleanupFailure": errorCode(snapshot.CleanupFailure),
		"target":            map[string]string{"namespace": "namespace-1", "pod": "pod-1", "container": "container-1"},
		"transportComplete": snapshot.Result.TransportComplete, "validatedStreamBytes": snapshot.Result.Bytes, "validatedEventFrames": snapshot.Result.DeliveredEvents}
	if snapshot.Intent.Kind != "" {
		doc["requested"] = map[string]any{"kind": snapshot.Intent.Kind, "paths": snapshot.Intent.Paths, "bounds": bounds(snapshot.Intent.Bounds)}
	}
	if m.SessionID != "" {
		doc["observed"] = map[string]any{"streamVersion": snapshot.Result.StreamVersion, "engineDigest": m.EngineDigest, "programmeDigest": m.ProgrammeDigest,
			"kind": m.Kind, "paths": m.Paths, "bounds": bounds(m.Bounds), "sessionStartedAt": m.SessionStartedAt, "deadline": m.Deadline}
	}
	summary, known := snapshot.Result.Summary()
	if known {
		doc["summary"] = summaryFields(summary)
		if summary.Incomplete {
			caveats = append(caveats, "Evidence is incomplete; retain the termination reason, unknown counters and hook coverage limitations.")
		}
	} else {
		doc["summary"] = nil
		caveats = append(caveats, "No validated terminal summary was received; engine counts and observation windows are unknown.")
	}
	if snapshot.Cleanup == traceclient.CleanupUnconfirmed {
		caveats = append(caveats, "API cleanup was not confirmed.")
	}
	doc["caveats"] = caveats
	data, err := json.Marshal(doc)
	if err != nil || len(data)+1 > MaxBytes {
		return Document{}, ErrInvalid
	}
	return Document{data: string(data)}, nil
}

func reportDigest(value string) bool {
	return len(value) == 71 && strings.HasPrefix(value, "sha256:") && strings.Trim(value[7:], "0123456789abcdef") == ""
}

func bounds(b trace.Bounds) any {
	return map[string]any{"durationNanos": int64(b.Duration), "events": b.Events, "outputBytes": b.OutputBytes, "mapBytes": b.MapBytes, "pathBytes": b.PathBytes}
}
func errorCode(err error) any {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	var known *traceclient.Error
	if errors.As(err, &known) {
		switch known.Kind {
		case traceclient.Invalid, traceclient.Configuration, traceclient.Unavailable, traceclient.Denied, traceclient.TargetChanged, traceclient.Capacity, traceclient.Gone, traceclient.Protocol, traceclient.Uncertain, traceclient.Incomplete:
			return string(known.Kind)
		}
	}
	return "unreported"
}
