// Package outputbuffer coalesces private worker pipe writes. It does not change
// protocol admission, accounting or framing; those remain owned by workeripc.
package outputbuffer

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/danushkastanley/kube-memlens/prototype/trace/workeripc"
)

const capacity = workeripc.MaxMessageBytes + 4
const flushDelay = 5 * time.Millisecond

// Writer holds at most one maximum-sized framed message. Its owner must bound
// underlying writes and Flush readiness and terminal messages synchronously.
// A partial batch is scheduled for flushing after five milliseconds. Scheduling
// and a blocked destination can delay delivery; the pipe deadline still applies.
type Writer struct {
	mu         sync.Mutex
	out        io.Writer
	data       [capacity]byte
	used       int
	err        error
	timer      *time.Timer
	generation uint64
	delay      time.Duration
}

func (*Writer) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "[private worker buffer]") }
func (*Writer) MarshalJSON() ([]byte, error) { return nil, workeripc.ErrProtocol }

func New(out io.Writer) *Writer { return &Writer{out: out, delay: flushDelay} }

func (w *Writer) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return 0, w.err
	}
	written := 0
	for len(data) > 0 {
		n := copy(w.data[w.used:], data)
		w.used += n
		written += n
		data = data[n:]
		if w.used == capacity {
			if err := w.flush(); err != nil {
				return written, err
			}
		}
	}
	if w.used > 0 && w.timer == nil {
		generation := w.generation
		w.timer = time.AfterFunc(w.delay, func() { w.flushPending(generation) })
	}
	return written, nil
}

func (w *Writer) flushPending(generation uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if generation == w.generation {
		// flush latches errors for every subsequent Write and Flush.
		_ = w.flush()
	}
}

func (w *Writer) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.flush()
}

// Abort discards pending data and waits for an in-progress bounded write. Once
// it returns, no timer can write again. The caller retains ownership of out.
func (w *Writer) Abort() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stopTimer()
	clear(w.data[:])
	w.used = 0
	if w.err == nil {
		w.err = io.ErrClosedPipe
	}
}

func (w *Writer) flush() error {
	w.stopTimer()
	if w.err != nil || w.used == 0 {
		return w.err
	}
	n, err := w.out.Write(w.data[:w.used])
	if err != nil || n != w.used {
		// Do not retain or expose destination errors, which may contain paths.
		w.err = workeripc.ErrOutput
	}
	clear(w.data[:])
	w.used = 0
	return w.err
}

func (w *Writer) stopTimer() {
	w.generation++
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
}
