// Package streamhttp implements bounded ephemeral HTTP frame writes.
package streamhttp

import (
	"context"
	"net/http"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

type Sink struct {
	writer  http.ResponseWriter
	control *http.ResponseController
}

func NewSink(writer http.ResponseWriter) (*Sink, error) {
	control := http.NewResponseController(writer)
	if err := control.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		return nil, err
	}
	writer.Header().Set("Content-Type", "application/x-ndjson")
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	return &Sink{writer, control}, nil
}
func (s *Sink) WriteFrame(ctx context.Context, data []byte) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	if len(data) == 0 || len(data) > traceframe.MaxBytes {
		return 0, traceframe.ErrInvalid
	}
	deadline := time.Now().Add(time.Second)
	if requested, ok := ctx.Deadline(); ok && requested.Before(deadline) {
		deadline = requested
	}
	if err := s.control.SetWriteDeadline(deadline); err != nil {
		return 0, err
	}
	interrupted := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = s.control.SetWriteDeadline(time.Now()); close(interrupted) })
	defer func() {
		if !stop() {
			<-interrupted
		}
	}()
	n, err := s.writer.Write(data)
	if err != nil {
		return n, err
	}
	if n != len(data) {
		return n, traceframe.ErrInvalid
	}
	if err := s.control.Flush(); err != nil {
		return n, err
	}
	return n, ctx.Err()
}
