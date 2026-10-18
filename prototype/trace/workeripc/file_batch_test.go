package workeripc

import (
	"bytes"
	"encoding/binary"
	"github.com/danushkastanley/kube-memlens/internal/trace"
	"io"
	"testing"
)

type batchCapture struct {
	capture
	calls, maximum int
	borrowed       []trace.FileActivity
}

func (c *batchCapture) FileActivities(events []trace.FileActivity) error {
	c.calls++
	c.maximum = max(c.maximum, len(events))
	c.borrowed = events
	for _, event := range events {
		if err := c.FileActivity(event); err != nil {
			return err
		}
	}
	return nil
}
func TestReadStreamGroupsOnlyBoundedCompleteFileMessages(t *testing.T) {
	request, data := fileBurst(t, 128)
	var output batchCapture
	if _, err := ReadStream(bytes.NewReader(data), request, &output, func() {}); err != nil {
		t.Fatal(err)
	}
	if len(output.files) != 128 || output.calls >= 128 || output.maximum > trace.MaxFileBatch {
		t.Fatal("batch lost events or exceeded its bound")
	}
	for _, event := range output.borrowed {
		if event != (trace.FileActivity{}) {
			t.Fatal("batch retained sensitive observations")
		}
	}
}

type gatedInput struct {
	first, rest []byte
	output      *batchCapture
	t           *testing.T
}

func (r *gatedInput) Read(p []byte) (int, error) {
	if len(r.first) > 0 {
		n := copy(p, r.first)
		r.first = r.first[n:]
		return n, nil
	}
	if len(r.output.files) != 1 {
		r.t.Fatal("reader blocked for more input before delivering sparse event")
	}
	if len(r.rest) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.rest)
	r.rest = r.rest[n:]
	return n, nil
}
func TestReadStreamDeliversBeforeReadingIncompleteNextMessage(t *testing.T) {
	for _, prefix := range []int{0, 2, 4, 8} {
		request, data := fileBurst(t, 1)
		offset := 0
		for range 2 {
			offset += 4 + int(binary.BigEndian.Uint32(data[offset:]))
		}
		var output batchCapture
		input := &gatedInput{first: data[:offset+prefix], rest: data[offset+prefix:], output: &output, t: t}
		if _, err := ReadStream(input, request, &output, func() {}); err != nil {
			t.Fatal(err)
		}
	}
}
func TestReadStreamKeepsPrefixBeforeMalformedBufferedMessage(t *testing.T) {
	request, data := fileBurst(t, 1)
	offset := 0
	for range 2 {
		offset += 4 + int(binary.BigEndian.Uint32(data[offset:]))
	}
	data = append(data[:offset], packet([]byte(`{"version":1,"type":"unknown"}`))...)
	var output batchCapture
	if _, err := ReadStream(bytes.NewReader(data), request, &output, func() {}); err != ErrProtocol || len(output.files) != 1 {
		t.Fatal("malformed buffered suffix hid valid prefix")
	}
}
func TestReadStreamDoesNotRetryFailedBatch(t *testing.T) {
	request, data := fileBurst(t, 128)
	output := batchCapture{capture: capture{fail: true}}
	if result, err := ReadStream(bytes.NewReader(data), request, &output, func() {}); err != ErrOutput || result.Counts.Produced != nil || output.calls != 1 {
		t.Fatal("failed output was retried or returned terminal evidence")
	}
	for _, event := range output.borrowed {
		if event != (trace.FileActivity{}) {
			t.Fatal("failed batch retained sensitive observations")
		}
	}
}
