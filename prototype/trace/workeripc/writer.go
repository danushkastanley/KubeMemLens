package workeripc

import (
	"fmt"
	"io"
	"sync"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceevidence"
)

// Writer serialises one child's observations. Its owner supplies a pipe with a
// bounded write deadline. There is no queue, retry, persistence or text logging.
// Ready and Result each have a separate one-message reserve; event bytes are
// additionally capped by the admitted output budget, independently of HTTP.
type Writer struct {
	mu       sync.Mutex
	out      io.Writer
	request  Request
	ready    bool
	finished bool
	failed   bool
	limited  bool
	events   uint64
	bytes    uint64
}

func (*Writer) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "[private worker writer]") }
func (*Writer) MarshalJSON() ([]byte, error) { return nil, ErrProtocol }

func NewWriter(out io.Writer, request Request) (*Writer, error) {
	if out == nil || request.Validate() != nil {
		return nil, ErrProtocol
	}
	return &Writer{out: out, request: request}, nil
}

func (w *Writer) write(message responseWire) error {
	if w.failed || w.finished || message.validate(w.request) != nil {
		w.failed = true
		return ErrProtocol
	}
	if message.Result != nil && !message.Result.covers(w.events) {
		w.failed = true
		return ErrProtocol
	}
	data, err := encode(message)
	if err != nil {
		w.failed = true
		return err
	}
	if message.Type == "file" || message.Type == "cache" {
		bounds := w.request.Specification.Bounds()
		if !w.ready {
			w.failed = true
			return ErrProtocol
		}
		if w.limited || w.events >= bounds.Events || uint64(len(data)) > bounds.OutputBytes-w.bytes {
			w.limited = true
			return ErrLimit
		}
		w.events++
		w.bytes += uint64(len(data))
	}
	if err := send(w.out, data); err != nil {
		w.failed = true
		return err
	}
	return nil
}

func (w *Writer) Ready() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ready {
		w.failed = true
		return ErrProtocol
	}
	if err := w.write(responseWire{Version: Version, Type: "ready"}); err != nil {
		return err
	}
	w.ready = true
	return nil
}

func (w *Writer) FileActivity(event trace.FileActivity) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if event.RequestedBytes == nil || event.CompletedBytes == nil {
		w.failed = true
		return ErrProtocol
	}
	return w.write(responseWire{Version: Version, Type: "file", File: &fileWire{event.ObservedAt.UTC(), event.Operation, *event.RequestedBytes, *event.CompletedBytes, event.Path.Reveal()}})
}

func (w *Writer) CacheActivity(event trace.CacheActivity) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.write(responseWire{Version: Version, Type: "cache", Cache: &cacheWire{event.ObservedAt.UTC(), event.Operation, event.Pages}})
}

func (w *Writer) OOMDecision(trace.OOMDecision) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.failed = true
	return ErrProtocol
}

// Finish must be called after adapter teardown. A message alone is not process
// teardown evidence: the supervisor must also confirm that the child was reaped.
func (w *Writer) Finish(result trace.Result) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if result.Version != trace.ContractVersion || (!w.ready && (result.Termination != trace.EngineFailed || !result.StartedAt.IsZero())) {
		w.failed = true
		return ErrProtocol
	}
	correlation, err := traceevidence.Encode(result.Correlation, result.StartedAt, result.EndedAt, w.request.Specification.Bounds().Duration)
	if err != nil {
		w.failed = true
		return ErrProtocol
	}
	c := result.Counts
	message := responseWire{Version: Version, Type: "result", Result: &resultWire{result.StartedAt.UTC(), result.EndedAt.UTC(), result.Termination, c.Produced, c.Sampled, c.Lost, c.Rejected, result.Incomplete, correlation}}
	if err := w.write(message); err != nil {
		return err
	}
	w.finished = true
	return nil
}
