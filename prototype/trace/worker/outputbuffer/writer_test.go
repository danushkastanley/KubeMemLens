package outputbuffer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/prototype/trace/workeripc"
)

type recordingWriter struct {
	bytes.Buffer
	writes int
	max    int
}

func (w *recordingWriter) Write(p []byte) (int, error) {
	w.writes++
	w.max = max(w.max, len(p))
	return w.Buffer.Write(p)
}

func manualWriter(out io.Writer) *Writer { return &Writer{out: out, delay: time.Hour} }

func TestCoalescesWritesAndBoundsEveryBatch(t *testing.T) {
	var out recordingWriter
	w := manualWriter(&out)
	defer w.Abort()
	input := bytes.Repeat([]byte("event"), capacity)
	for start := 0; start < len(input); start += 17 {
		chunk := input[start:min(start+17, len(input))]
		if n, err := w.Write(chunk); n != len(chunk) || err != nil {
			t.Fatalf("write: %d %v", n, err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), input) || out.writes != 5 || out.max != capacity {
		t.Fatalf("changed bytes or unbounded batches: %d writes, maximum %d", out.writes, out.max)
	}
	if w.data != [capacity]byte{} {
		t.Fatal("flushed private bytes retained")
	}
}

type notifyWriter chan []byte

func (w notifyWriter) Write(p []byte) (int, error) {
	w <- bytes.Clone(p)
	return len(p), nil
}

func TestSparseOutputFlushesWithoutAnotherWrite(t *testing.T) {
	out := make(notifyWriter, 1)
	w := New(out)
	defer w.Abort()
	if _, err := w.Write([]byte("sparse")); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-out:
		if string(got) != "sparse" {
			t.Fatal("changed sparse output")
		}
	case <-time.After(time.Second):
		t.Fatal("partial batch was never flushed")
	}
}

type failingWriter struct {
	calls int
	err   error
}

func (w *failingWriter) Write(p []byte) (int, error) {
	w.calls++
	return len(p) - 1, w.err
}

func TestAsyncFailureIsStickyAndRedacted(t *testing.T) {
	for _, underlying := range []error{nil, errors.New("/private/destination")} {
		out := &failingWriter{err: underlying}
		w := manualWriter(out)
		defer w.Abort()
		_, _ = w.Write([]byte("private-payload"))
		w.flushPending(w.generation)
		if w.Flush() != workeripc.ErrOutput {
			t.Fatal("asynchronous short/failed write was hidden")
		}
		if n, err := w.Write([]byte("again")); n != 0 || err != workeripc.ErrOutput || out.calls != 1 {
			t.Fatal("failed destination was retried")
		}
		if w.data != [capacity]byte{} {
			t.Fatal("failed batch retained private bytes")
		}
		if strings.Contains(fmt.Sprintf("%+v %#v", w, w), "private-payload") {
			t.Fatal("formatting exposed private bytes")
		}
		if _, err := json.Marshal(w); err == nil {
			t.Fatal("JSON exposed private state")
		}
	}
}

func TestExplicitFlushInvalidatesEarlierTimer(t *testing.T) {
	var out recordingWriter
	w := manualWriter(&out)
	defer w.Abort()
	_, _ = w.Write([]byte("ready"))
	old := w.generation
	if w.Flush() != nil || out.String() != "ready" {
		t.Fatal("readiness was not physically flushed")
	}
	_, _ = w.Write([]byte("event"))
	w.flushPending(old)
	if out.String() != "ready" {
		t.Fatal("stale timer flushed a new batch")
	}
	if w.Flush() != nil || out.String() != "readyevent" {
		t.Fatal("final batch was not flushed")
	}
}

func TestAbortDiscardsPendingOutput(t *testing.T) {
	var out recordingWriter
	w := manualWriter(&out)
	_, _ = w.Write([]byte("private-payload"))
	old := w.generation
	w.Abort()
	w.flushPending(old)
	w.Abort()
	if w.Flush() != io.ErrClosedPipe || out.Len() != 0 || w.data != [capacity]byte{} {
		t.Fatal("aborted bytes survived or were delivered")
	}
	if n, err := w.Write([]byte("again")); n != 0 || err != io.ErrClosedPipe {
		t.Fatal("aborted buffer accepted output")
	}
}

func TestFullBatchFailureStopsLargeWrite(t *testing.T) {
	out := &failingWriter{}
	w := manualWriter(out)
	defer w.Abort()
	n, err := w.Write(make([]byte, capacity*3))
	if n != capacity || err != workeripc.ErrOutput || out.calls != 1 {
		t.Fatalf("failed batch was retried: %d %v, calls %d", n, err, out.calls)
	}
}
