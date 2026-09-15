package traceframe

import (
	"bufio"
	"encoding/json"
	"github.com/danushkastanley/kube-memlens/internal/traceevidence"
	"io"
	"time"
)

// Reader validates frame order, immutable disclosure policy and cumulative
// counts. The caller must give its underlying transport bounded read deadlines.
// EOF before a summary is always incomplete, including an empty response.
type Reader struct {
	input         *bufio.Reader
	metadata      *wireMetadata
	bytes, events uint64
	ended, failed bool
	version       int
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
		if e.Type != MetadataFrame || size+uint64(Reserve(e.Version)) > e.Metadata.Bounds.OutputBytes {
			return ErrInvalid
		}
		r.metadata = e.Metadata
		r.version = e.Version
		return nil
	}
	if e.Version != r.version || r.bytes+size > r.metadata.Bounds.OutputBytes {
		return ErrInvalid
	}
	switch e.Type {
	case EventFrame:
		if r.version == AggregateVersion && (r.metadata.Kind != "files" || r.metadata.Paths != "confirmed") {
			return ErrInvalid
		}
		if r.bytes+size+uint64(Reserve(r.version)) > r.metadata.Bounds.OutputBytes || r.events >= r.metadata.Bounds.Events {
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
		if size > uint64(Reserve(r.version)) || summary.WrittenEvents != r.events || summary.WrittenBytesBeforeSummary != r.bytes || summary.SessionEndedAt.Before(r.metadata.SessionStartedAt) {
			return ErrInvalid
		}
		if summary.ObservationStartedAt != nil && summary.ObservationStartedAt.Before(r.metadata.SessionStartedAt) {
			return ErrInvalid
		}
		if r.version == AggregateVersion {
			var start, end time.Time
			if summary.ObservationStartedAt != nil && summary.ObservationEndedAt != nil {
				start, end = *summary.ObservationStartedAt, *summary.ObservationEndedAt
			}
			correlation, err := traceevidence.Decode(summary.Correlation, start, end, r.metadata.bounds().Duration)
			if err != nil || (correlation != nil && correlation.State == "overlapping" && correlation.EvidenceStart.Before(r.metadata.SessionStartedAt)) {
				return ErrInvalid
			}
			if summary.ObservationEndedAt != nil && summary.ObservationEndedAt.After(r.metadata.Deadline) {
				return ErrInvalid
			}
			if aggregateCount(*summary) > r.metadata.Bounds.Events || (summary.FileAggregates != nil && r.metadata.Kind != "files") || (summary.CacheAggregates != nil && r.metadata.Kind != "cache") {
				return ErrInvalid
			}
			if r.metadata.Paths == "confirmed" && summary.FileAggregates != nil && aggregateCount(*summary) != r.events {
				return ErrInvalid
			}
		}
		r.ended = true
	default:
		return ErrInvalid
	}
	return nil
}
func (r *Reader) Counts() (bytes, events uint64) { return r.bytes, r.events }
