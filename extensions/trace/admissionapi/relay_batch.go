package admissionapi

import (
	"context"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

// forwardEvents coalesces only already-buffered complete events. It never waits
// for more data before flushing. Metadata and the terminal summary are separate
// writes, and summary EOF/lease validation remains owned by run.
func (r *relay) forwardEvents(ctx context.Context, first traceframe.Frame) (traceframe.Frame, error) {
	data, err := traceframe.Encode(first)
	if err != nil {
		return traceframe.Frame{}, admission.ErrUnavailable
	}
	used := copy(r.batch[:], data)
	events := uint64(1)
	defer func() { clear(r.batch[:used]) }()
	for {
		next, available, err := r.reader.NextBuffered()
		if err != nil {
			// Preserve the delivery of preceding valid events, as in the
			// single-frame path, then report the rejected upstream frame.
			if writeErr := r.write(ctx, r.batch[:used], events); writeErr != nil {
				return traceframe.Frame{}, writeErr
			}
			return traceframe.Frame{}, admission.ErrUnavailable
		}
		if !available {
			break
		}
		if next.Type() != traceframe.EventFrame {
			return next, r.write(ctx, r.batch[:used], events)
		}
		encoded, err := traceframe.Encode(next)
		if err != nil {
			return traceframe.Frame{}, admission.ErrUnavailable
		}
		if len(encoded) > len(r.batch)-used {
			return next, r.write(ctx, r.batch[:used], events)
		}
		used += copy(r.batch[used:], encoded)
		events++
	}
	if err := r.write(ctx, r.batch[:used], events); err != nil {
		return traceframe.Frame{}, err
	}
	next, err := r.reader.Next()
	if err != nil {
		return traceframe.Frame{}, admission.ErrUnavailable
	}
	return next, nil
}
