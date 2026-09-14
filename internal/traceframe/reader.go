package traceframe

import (
	"bufio"
	"encoding/json"
	"io"
)

// Reader validates frame order, immutable disclosure policy and cumulative
// counts. The caller must give its underlying transport bounded read deadlines.
// EOF before a summary is always incomplete, including an empty response.
type Reader struct {
	input         *bufio.Reader
	metadata      *wireMetadata
	bytes, events uint64
	ended, failed bool
}

func NewReader(input io.Reader) *Reader { return &Reader{input: bufio.NewReaderSize(input, MaxBytes)} }
func (r *Reader) Next() (Frame, error) {
	if r.failed {
		return Frame{}, ErrInvalid
	}
	data, err := r.input.ReadSlice('\n')
	if err != nil {
		if err == io.EOF && len(data) == 0 && r.ended {
			return Frame{}, io.EOF
		}
		r.failed = true
		return Frame{}, io.ErrUnexpectedEOF
	}
	if r.ended {
		r.failed = true
		return Frame{}, ErrInvalid
	}
	frame, err := Decode(data)
	if err != nil {
		r.failed = true
		return Frame{}, err
	}
	var e envelope
	if json.Unmarshal(data, &e) != nil {
		r.failed = true
		return Frame{}, ErrInvalid
	}
	if r.accept(e, uint64(len(data))) != nil {
		r.failed = true
		return Frame{}, ErrInvalid
	}
	r.bytes += uint64(len(data))
	return frame, nil
}
func (r *Reader) accept(e envelope, size uint64) error {
	if r.metadata == nil {
		if e.Type != MetadataFrame || size+TerminalReserve > e.Metadata.Bounds.OutputBytes {
			return ErrInvalid
		}
		r.metadata = e.Metadata
		return nil
	}
	if r.bytes+size > r.metadata.Bounds.OutputBytes {
		return ErrInvalid
	}
	switch e.Type {
	case EventFrame:
		if r.bytes+size+TerminalReserve > r.metadata.Bounds.OutputBytes || r.events >= r.metadata.Bounds.Events {
			return ErrInvalid
		}
		event := e.Event
		if event.ObservedAt.Before(r.metadata.SessionStartedAt) || event.ObservedAt.After(r.metadata.Deadline) {
			return ErrInvalid
		}
		switch r.metadata.Kind {
		case "files":
			if event.File == nil {
				return ErrInvalid
			}
			if r.metadata.Paths == "omit" && event.File.Path != nil {
				return ErrInvalid
			}
			if r.metadata.Paths == "confirmed" {
				if event.File.Path == nil {
					return ErrInvalid
				}
				if _, err := DecodeText(*event.File.Path, r.metadata.Bounds.PathBytes); err != nil {
					return ErrInvalid
				}
			}
		case "cache":
			if event.Cache == nil {
				return ErrInvalid
			}
		case "oom":
			if event.OOM == nil {
				return ErrInvalid
			}
		}
		r.events++
	case SummaryFrame:
		summary := e.Summary
		if summary.Termination == "expired" && summary.SessionEndedAt.Before(r.metadata.Deadline) {
			return ErrInvalid
		}
		if size > TerminalReserve || summary.WrittenEvents != r.events || summary.WrittenBytesBeforeSummary != r.bytes || summary.SessionEndedAt.Before(r.metadata.SessionStartedAt) {
			return ErrInvalid
		}
		if summary.ObservationStartedAt != nil && summary.ObservationStartedAt.Before(r.metadata.SessionStartedAt) {
			return ErrInvalid
		}
		r.ended = true
	default:
		return ErrInvalid
	}
	return nil
}
func (r *Reader) Counts() (bytes, events uint64) { return r.bytes, r.events }
