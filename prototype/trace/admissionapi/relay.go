package admissionapi

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
	"github.com/danushkastanley/kube-memlens/prototype/trace/streamhttp"
)

type relay struct {
	reader          *traceframe.Reader
	sink            *streamhttp.Sink
	lease           *admission.Lease
	written, events uint64
	transportFailed bool
}

func (r *relay) forward(ctx context.Context, frame traceframe.Frame) error {
	data, err := traceframe.Encode(frame)
	if err != nil {
		return admission.ErrUnavailable
	}
	n, err := r.sink.WriteFrame(ctx, data)
	if n >= 0 && n <= len(data) {
		r.written += uint64(n)
	}
	if err != nil || n != len(data) {
		r.transportFailed = true
		return admission.ErrUnavailable
	}
	if frame.Type() == traceframe.EventFrame {
		r.events++
	}
	return nil
}
func (r *relay) run(ctx context.Context, first traceframe.Frame) error {
	frame := first
	for {
		if frame.Type() == traceframe.SummaryFrame {
			if _, err := r.reader.Next(); err != io.EOF {
				return admission.ErrUnavailable
			}
			if err := r.lease.RevalidateStream(ctx); err != nil {
				return err
			}
		}
		if err := r.forward(ctx, frame); err != nil {
			return err
		}
		if frame.Type() == traceframe.SummaryFrame {
			return nil
		}
		var err error
		frame, err = r.reader.Next()
		if err != nil {
			return admission.ErrUnavailable
		}
	}
}

// A control-side failure can still terminate a healthy downstream connection.
// It reports only known delivery counts and an unknown observation window. No
// summary is appended after a failed/partial downstream frame write.
func (r *relay) terminate(parent context.Context, reason trace.Termination) error {
	if r.transportFailed || parent.Err() != nil {
		return admission.ErrUnavailable
	}
	summary, err := traceframe.NewSummary(traceframe.Summary{SessionEndedAt: time.Now().UTC(), Termination: reason, WrittenEvents: r.events, WrittenBytesBeforeSummary: r.written, Incomplete: true})
	if err != nil {
		return admission.ErrUnavailable
	}
	data, err := traceframe.Encode(summary)
	if err != nil || r.written+uint64(len(data)) > r.lease.Admission().Specification().Bounds().OutputBytes {
		return admission.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	return r.forward(ctx, summary)
}
func validationTermination(err error) trace.Termination {
	if errors.Is(err, admission.ErrTargetChanged) {
		return trace.TargetChanged
	}
	return trace.AuthorisationLost
}
