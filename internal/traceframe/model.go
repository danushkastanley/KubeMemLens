// Package traceframe owns the explicitly encoded ephemeral NDJSON contract.
// Ordinary JSON and formatted logs must never expose a frame's contents.
package traceframe

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

const Version = 1
const MaxBytes = 8 << 10
const TerminalReserve = 2 << 10

var ErrInvalid = errors.New("invalid trace frame")

type Type string

const (
	MetadataFrame Type = "metadata"
	EventFrame    Type = "event"
	SummaryFrame  Type = "summary"
)

type Metadata struct {
	SessionID        string
	EngineDigest     string
	ProgrammeDigest  string
	Specification    trace.Specification
	SessionStartedAt time.Time
	Deadline         time.Time
}

type Summary struct {
	SessionEndedAt       time.Time
	ObservationStartedAt *time.Time
	ObservationEndedAt   *time.Time
	Termination          trace.Termination
	EngineCounts         trace.Counts
	WrittenEvents        uint64
	RejectedEvents       uint64
	// WrittenBytesBeforeSummary includes metadata, event frames and newlines.
	// The reader adds the summary's actual byte length for the full stream total.
	WrittenBytesBeforeSummary uint64
	Incomplete                bool
}

// Frame has immutable encoded storage; Encode is the only disclosure operation.
// Constructors validate before allocating a frame. No collector serializer may
// accidentally persist it through json.Marshal or fmt.
type Frame struct {
	kind Type
	data string
}

func (f Frame) Type() Type                    { return f.kind }
func (Frame) Format(w fmt.State, _ rune)      { _, _ = io.WriteString(w, "[ephemeral trace frame]") }
func (Frame) MarshalJSON() ([]byte, error)    { return nil, ErrInvalid }
func (Metadata) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "[trace metadata]") }
func (Metadata) MarshalJSON() ([]byte, error) { return nil, ErrInvalid }
func (Summary) Format(w fmt.State, _ rune)    { _, _ = io.WriteString(w, "[trace summary]") }
func (Summary) MarshalJSON() ([]byte, error)  { return nil, ErrInvalid }

// Encode returns an owned copy including the NDJSON newline. Call only at an
// authenticated ephemeral transport boundary; bytes must not be logged/stored.
func Encode(f Frame) ([]byte, error) {
	if f.data == "" || len(f.data) > MaxBytes {
		return nil, ErrInvalid
	}
	return []byte(f.data), nil
}
