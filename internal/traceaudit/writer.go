package traceaudit

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
)

var ErrUnavailable = errors.New("trace audit delivery unavailable")

const QueueCapacity = 64

type writeRequest struct {
	record  Record
	receipt chan error
}

// Writer has one worker and a fixed queue. A destination that blocks can retain
// at most that worker and the bounded queue; callers still honour their deadlines.
// Any uncertain delivery latches unhealthy until the controller is restarted.
// A receipt confirms the writer accepted all bytes, not durable log storage.
type Writer struct {
	mu             sync.Mutex
	destination    io.Writer
	queue          chan writeRequest
	stop, done     chan struct{}
	failed, closed atomic.Bool
	once           sync.Once
}

func NewWriter(destination io.Writer) (*Writer, error) {
	if destination == nil {
		return nil, ErrInvalid
	}
	w := &Writer{destination: destination, queue: make(chan writeRequest, QueueCapacity), stop: make(chan struct{}), done: make(chan struct{})}
	go w.run()
	return w, nil
}
func (w *Writer) Healthy() error {
	if w == nil || w.failed.Load() || w.closed.Load() {
		return ErrUnavailable
	}
	return nil
}
func (w *Writer) Write(ctx context.Context, record Record) error {
	if w == nil {
		return ErrUnavailable
	}
	if record.data == "" {
		return ErrInvalid
	}
	w.mu.Lock()
	if w.Healthy() != nil || ctx.Err() != nil {
		w.mu.Unlock()
		return ErrUnavailable
	}
	request := writeRequest{record: record, receipt: make(chan error, 1)}
	select {
	case w.queue <- request:
		w.mu.Unlock()
	default:
		w.failed.Store(true)
		w.mu.Unlock()
		return ErrUnavailable
	}
	select {
	case err := <-request.receipt:
		return err
	case <-ctx.Done():
		w.failed.Store(true)
		return ErrUnavailable
	case <-w.stop:
		return ErrUnavailable
	}
}
func (w *Writer) run() {
	defer close(w.done)
	defer w.rejectQueued()
	for {
		select {
		case <-w.stop:
			return
		case request := <-w.queue:
			if w.Healthy() != nil {
				request.receipt <- ErrUnavailable
				continue
			}
			data, _ := request.record.Bytes()
			n, err := w.destination.Write(data)
			if err != nil || n != len(data) {
				w.failed.Store(true)
			}
			request.receipt <- w.Healthy()
		}
	}
}

func (w *Writer) rejectQueued() {
	for {
		select {
		case request := <-w.queue:
			w.failed.Store(true)
			request.receipt <- ErrUnavailable
		default:
			return
		}
	}
}
func (w *Writer) Close(ctx context.Context) error {
	if w == nil {
		return nil
	}
	w.once.Do(func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.closed.Store(true)
		close(w.stop)
	})
	select {
	case <-w.done:
		if w.failed.Load() {
			return ErrUnavailable
		}
		return nil
	case <-ctx.Done():
		return ErrUnavailable
	}
}
