// Package incidentsession owns bounded, ephemeral incident records. Transport
// adapters must derive principals and evidence receipts from authorised sources.
package incidentsession

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/tracereport"
)

const SchemaVersion = 1
const TraceExportSchema = 2
const MaxEntryBytes = 4096
const MaxExportBytes = 256 << 10
const MaxEntries = 128

var (
	ErrInvalid     = errors.New("invalid incident session request")
	ErrDenied      = errors.New("incident session access denied")
	ErrNotFound    = errors.New("incident session unavailable")
	ErrCapacity    = errors.New("incident session capacity reached")
	ErrClosed      = errors.New("incident session is closed")
	ErrStopped     = errors.New("incident session store is stopped")
	ErrUnavailable = errors.New("incident session service unavailable")
	ErrChanged     = errors.New("incident evidence source changed")
	ErrDisabled    = errors.New("incident evidence source is disabled")
	ErrUnsupported = errors.New("incident evidence source is unsupported")
)

type Principal struct {
	Namespace, NamespaceUID, Actor string
}

type Operation string

const (
	Create           Operation = "create"
	Inspect          Operation = "inspect"
	Capture          Operation = "capture"
	Compare          Operation = "compare"
	Markers          Operation = "markers"
	ReferenceTrace   Operation = "trace-reference"
	Append           Operation = "append"
	CloseSession     Operation = "close"
	Delete           Operation = "delete"
	ExportSanitised  Operation = "export-sanitised"
	ExportAuthorised Operation = "export-authorised"
)

// Authorizer is called for every operation, before looking up private state.
// It must use current authority; possession of an ID never grants access.
type Authorizer interface {
	Authorize(context.Context, Principal, Operation, string) error
}

type Limits struct {
	Sessions, NamespaceSessions, Entries, SessionBytes int
	Retention                                          time.Duration
}

func DefaultLimits() Limits {
	return Limits{Sessions: 64, NamespaceSessions: 8, Entries: MaxEntries, SessionBytes: MaxExportBytes, Retention: time.Hour}
}

type Kind string

const (
	Opened          Kind = "opened"
	Annotated       Kind = "annotated"
	Captured        Kind = "captured"
	Compared        Kind = "compared"
	Marked          Kind = "marked"
	TraceReferenced Kind = "trace-referenced"
	Gap             Kind = "gap"
	Closed          Kind = "closed"
)

// CaptureReference contains capture provenance, never a file path, URL or raw trace data.
// A server adapter must validate/acquire the actual evidence before constructing
// this receipt. The store enforces its session tenant and bounded schema shape.
type CaptureReference struct {
	NamespaceUID  string    `json:"namespaceUID"`
	Digest        string    `json:"digest"`
	SchemaVersion int       `json:"schemaVersion"`
	ObservedAt    time.Time `json:"observedAt"`
}

type Input struct {
	Kind           Kind
	Source         string
	ObservedAt     time.Time
	Note           string
	References     []CaptureReference
	GapReason      string
	traceReference *tracereport.Reference
}

// EntryStatus makes failed or partial acquisition visible in action responses
// without disclosing annotations, evidence bytes or resource identities.
type EntryStatus struct {
	Sequence       uint64 `json:"sequence"`
	Kind           Kind   `json:"kind"`
	Source         string `json:"source"`
	GapReason      string `json:"gapReason,omitempty"`
	ClockUncertain bool   `json:"clockUncertain"`
}

type Summary struct {
	ID           string      `json:"id"`
	OpenedAt     time.Time   `json:"openedAt"`
	ExpiresAt    time.Time   `json:"expiresAt"`
	ClosedAt     *time.Time  `json:"closedAt,omitempty"`
	Entries      int         `json:"entries"`
	LimitReached bool        `json:"limitReached"`
	Latest       EntryStatus `json:"latest"`
}

func (Principal) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "[incident principal]") }
func (Input) Format(w fmt.State, _ rune)     { _, _ = io.WriteString(w, "[private incident input]") }
func (CaptureReference) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[private evidence reference]")
}

func (*Store) Format(w fmt.State, _ rune)  { _, _ = io.WriteString(w, "[incident session store]") }
func (Summary) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "[incident session summary]") }

func (Entry) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "[private incident entry]") }
func (AuthorisedExport) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[authorised incident export]")
}

type Entry struct {
	Sequence       uint64                 `json:"sequence"`
	Kind           Kind                   `json:"kind"`
	RecordedAt     time.Time              `json:"recordedAt"`
	ObservedAt     *time.Time             `json:"observedAt"`
	Namespace      string                 `json:"namespace"`
	NamespaceUID   string                 `json:"namespaceUID,omitempty"`
	Actor          string                 `json:"actor"`
	Source         string                 `json:"source"`
	Sensitivity    string                 `json:"sensitivity"`
	ClockUncertain bool                   `json:"clockUncertain"`
	Note           string                 `json:"note,omitempty"`
	References     []CaptureReference     `json:"references,omitempty"`
	GapReason      string                 `json:"gapReason,omitempty"`
	TraceReference *tracereport.Reference `json:"traceReference,omitempty"`
}

type record struct {
	principal       Principal
	id              string
	opened, expires time.Time
	closed          *time.Time
	entries         []Entry
	captures        []CapturedEvidence
	limitReached    bool
}

func (r *record) summary() Summary {
	latest := r.entries[len(r.entries)-1]
	value := Summary{Latest: EntryStatus{Sequence: latest.Sequence, Kind: latest.Kind, Source: latest.Source, GapReason: latest.GapReason, ClockUncertain: latest.ClockUncertain}, ID: r.id, OpenedAt: r.opened.UTC(), ExpiresAt: r.expires.UTC(), Entries: len(r.entries), LimitReached: r.limitReached}
	if r.closed != nil {
		at := *r.closed
		value.ClosedAt = &at
	}
	return value
}
