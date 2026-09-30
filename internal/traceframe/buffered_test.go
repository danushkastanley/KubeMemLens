package traceframe

import (
	"bytes"
	"io"
	"testing"
)

type countedReader struct {
	io.Reader
	calls int
}

func (r *countedReader) Read(p []byte) (int, error) { r.calls++; return r.Reader.Read(p) }

func TestBufferedFramesUseIdenticalValidationWithoutReadingTransport(t *testing.T) {
	data := stream(t)
	input := &countedReader{Reader: bytes.NewReader(data)}
	r := NewReader(input)
	if _, ready, err := r.NextBuffered(); ready || err != nil || input.calls != 0 {
		t.Fatal("empty buffer performed transport read")
	}
	if _, err := r.Next(); err != nil {
		t.Fatal(err)
	}
	reads := input.calls
	for _, kind := range []Type{EventFrame, SummaryFrame} {
		frame, ready, err := r.NextBuffered()
		if !ready || err != nil || frame.Type() != kind || input.calls != reads {
			t.Fatal("buffered frame changed or read transport")
		}
	}
	if _, ready, err := r.NextBuffered(); ready || err != nil || input.calls != reads {
		t.Fatal("buffered read tried to prove EOF")
	}
	if _, err := r.Next(); err != io.EOF {
		t.Fatal("terminal EOF was not checked")
	}
	if n, events := r.Counts(); n != uint64(len(data)) || events != 1 {
		t.Fatal("buffered accounting differs")
	}
}

func TestBufferedReadLeavesPartialFrameForNext(t *testing.T) {
	lines := bytes.SplitAfter(stream(t), []byte("\n"))
	first := append(bytes.Clone(lines[0]), lines[1][:10]...)
	input := &countedReader{Reader: io.MultiReader(bytes.NewReader(first), bytes.NewReader(lines[1][10:]))}
	r := NewReader(input)
	if _, err := r.Next(); err != nil {
		t.Fatal(err)
	}
	reads := input.calls
	if _, ready, err := r.NextBuffered(); ready || err != nil || input.calls != reads {
		t.Fatal("partial frame caused read or was consumed")
	}
	if frame, err := r.Next(); err != nil || frame.Type() != EventFrame {
		t.Fatal("partial frame did not survive")
	}
}

func TestBufferedFramesCannotBypassOrderAndAccounting(t *testing.T) {
	data := stream(t)
	for _, bad := range [][]byte{
		bytes.Replace(data, []byte(`"writtenEvents":1`), []byte(`"writtenEvents":0`), 1),
		append(bytes.Clone(data), []byte("{}\n")...),
	} {
		r := NewReader(bytes.NewReader(bad))
		if _, err := r.Next(); err != nil {
			t.Fatal(err)
		}
		for {
			_, ready, err := r.NextBuffered()
			if err != nil {
				break
			}
			if !ready {
				t.Fatal("invalid buffered frame was accepted")
			}
		}
		if _, _, err := r.NextBuffered(); err == nil {
			t.Fatal("failed buffered reader resumed")
		}
	}
}
