package tracereport

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceclient"
	"github.com/danushkastanley/kube-memlens/internal/tracecompat"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

const MinimumSchema = 1
const CurrentSchema = 2

var ErrVersion = errors.New("unsupported trace export schema")

// Archive is a bounded, structurally validated operator file, not authenticated
// provenance or proof that its numbers were measured. Its text remains untrusted.
// It is separate from Document, which is produced from a validated live session.
type Archive struct {
	data   string
	schema int
}

func (Archive) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "[private trace archive]") }
func (Archive) MarshalJSON() ([]byte, error) { return nil, ErrInvalid }
func (a Archive) SchemaVersion() int         { return a.schema }
func (a Archive) Bytes() ([]byte, error) {
	if a.data == "" {
		return nil, ErrInvalid
	}
	return []byte(a.data), nil
}

// Read preserves the original bytes, including allowed additive caveat text.
// No new field, private event row or policy-critical default is inferred.
func Read(reader io.Reader) (Archive, error) {
	if reader == nil {
		return Archive{}, ErrInvalid
	}
	data, err := io.ReadAll(io.LimitReader(reader, MaxBytes+1))
	if err != nil || len(data) > MaxBytes {
		return Archive{}, ErrInvalid
	}
	var wire archiveWire
	if jsonv2.Unmarshal(data, &wire, jsonv2.RejectUnknownMembers(true)) != nil {
		return Archive{}, ErrInvalid
	}
	if wire.SchemaVersion < MinimumSchema || wire.SchemaVersion > CurrentSchema {
		return Archive{}, ErrVersion
	}
	if archiveFields(data, wire.SchemaVersion) != nil {
		return Archive{}, ErrInvalid
	}
	if err := wire.validate(); err != nil {
		return Archive{}, err
	}
	return Archive{data: string(data), schema: wire.SchemaVersion}, nil
}

func archiveFields(data []byte, version int) error {
	var fields map[string]json.RawMessage
	if jsonv2.Unmarshal(data, &fields) != nil {
		return ErrInvalid
	}
	required := []string{"schemaVersion", "kind", "capturedAt", "toolVersion", "redacted", "state", "cleanup", "failure", "cleanupFailure", "target", "summary", "caveats", "transportComplete", "validatedStreamBytes", "validatedEventFrames"}
	if version == CurrentSchema {
		required = append(required, "contractVersion")
	} else if _, present := fields["contractVersion"]; present {
		return ErrInvalid
	}
	for _, key := range required {
		if _, present := fields[key]; !present {
			return ErrInvalid
		}
	}
	for key, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) && key != "failure" && key != "cleanupFailure" && key != "summary" && key != "contractVersion" {
			return ErrInvalid
		}
	}
	return nil
}
func (w archiveWire) validate() error {
	if w.Kind != "TraceReport" || !w.Redacted || w.CapturedAt.IsZero() || !validToolVersion(w.ToolVersion) || !w.State.Terminal() {
		return ErrInvalid
	}
	if w.Cleanup != traceclient.CleanupConfirmed && w.Cleanup != traceclient.CleanupUnconfirmed && w.Cleanup != traceclient.CleanupNotRequested {
		return ErrInvalid
	}
	if !archiveError(w.Failure, w.SchemaVersion) || !archiveError(w.CleanupFailure, w.SchemaVersion) {
		return ErrInvalid
	}
	if w.ContractVersion != nil && *w.ContractVersion != uint16(tracecompat.Current) {
		return ErrVersion
	}
	if w.Target.Namespace != "namespace-1" || w.Target.Pod != "pod-1" || w.Target.Container != "container-1" || len(w.Caveats) < 1 || len(w.Caveats) > 32 {
		return ErrInvalid
	}
	for _, text := range w.Caveats {
		if len(text) > 1024 || !utf8.ValidString(text) || strings.ContainsAny(text, "\x00\r\n\x1b") {
			return ErrInvalid
		}
	}
	if w.ValidatedStreamBytes > 32<<20 || w.ValidatedEventFrames > 100000 {
		return ErrInvalid
	}
	if w.Requested != nil && w.Requested.validate() != nil {
		return ErrInvalid
	}
	if w.Observed == nil {
		if w.TransportComplete || w.ValidatedStreamBytes != 0 || w.ValidatedEventFrames != 0 || !bytes.Equal(w.Summary, []byte("null")) {
			return ErrInvalid
		}
		if w.State == traceclient.StateCompleted || w.State == traceclient.StateTruncated {
			return ErrInvalid
		}
		return nil
	}
	o := w.Observed
	if o.validate() != nil || w.ValidatedStreamBytes > o.Bounds.OutputBytes || w.ValidatedEventFrames > o.Bounds.Events {
		return ErrInvalid
	}
	if w.Requested != nil && *w.Requested != o.intentWire {
		return ErrInvalid
	}
	if w.ContractVersion != nil && !tracecompat.StreamCompatible(o.Kind, o.StreamVersion) {
		return ErrInvalid
	}
	if o.StreamVersion == 2 && o.Paths == trace.OmitPaths && w.ValidatedEventFrames != 0 {
		return ErrInvalid
	}
	if bytes.Equal(w.Summary, []byte("null")) {
		if w.TransportComplete || w.State == traceclient.StateCompleted || w.State == traceclient.StateTruncated {
			return ErrInvalid
		}
		return nil
	}
	summary, encodedBytes, err := readSummary(w.Summary, *o)
	if err != nil || summary.WrittenEvents != w.ValidatedEventFrames || summary.WrittenBytesBeforeSummary+encodedBytes > w.ValidatedStreamBytes {
		return ErrInvalid
	}
	if w.TransportComplete && summary.WrittenBytesBeforeSummary+encodedBytes != w.ValidatedStreamBytes {
		return ErrInvalid
	}
	switch w.State {
	case traceclient.StateCompleted:
		if !w.TransportComplete || summary.Termination != trace.Expired || w.Failure != nil {
			return ErrInvalid
		}
	case traceclient.StateTruncated:
		if !w.TransportComplete || (summary.Termination != trace.EventLimit && summary.Termination != trace.OutputLimit) {
			return ErrInvalid
		}
	}
	return nil
}
func (w intentWire) validate() error { return trace.ValidateIntent(w.Kind, w.Paths, w.Bounds.domain()) }
func (w observedWire) validate() error {
	if w.intentWire.validate() != nil || !traceframe.AllowsKind(w.StreamVersion, w.Kind) || !reportDigest(w.EngineDigest) || !reportDigest(w.ProgrammeDigest) || w.SessionStartedAt.IsZero() || !w.Deadline.After(w.SessionStartedAt) || w.Deadline.Sub(w.SessionStartedAt) > w.Bounds.domain().Duration {
		return ErrInvalid
	}
	return nil
}
func validToolVersion(value string) bool {
	return len(value) > 0 && len(value) <= 256 && utf8.ValidString(value) && !strings.ContainsAny(value, "\x00\r\n\x1b")
}
func archiveError(value *string, version int) bool {
	if value == nil {
		return true
	}
	switch *value {
	case "invalid", "configuration", "unavailable", "denied", "target_changed", "capacity", "gone", "invalid_response", "outcome_unknown", "incomplete", "cancelled", "deadline_exceeded", "unreported":
		return true
	case "incompatible":
		return version >= 2
	default:
		return false
	}
}
