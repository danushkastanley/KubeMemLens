package workeripc

import (
	"bytes"
	"io"
	"os"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

type countedInput struct {
	io.Reader
	calls, maximum int
}

func (r *countedInput) Read(p []byte) (int, error) {
	r.calls++
	r.maximum = max(r.maximum, len(p))
	return r.Reader.Read(p)
}

func fileBurst(t testing.TB, count int) (Request, []byte) {
	t.Helper()
	request := requestFixture(t, trace.Files, trace.ConfirmedPaths, trace.DefaultBounds())
	var data bytes.Buffer
	writer, err := NewWriter(&data, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Ready(); err != nil {
		t.Fatal(err)
	}
	for range count {
		if err := writer.FileActivity(fileFixture(request, "/work/fixed-seed.bin")); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Finish(resultFixture(request)); err != nil {
		t.Fatal(err)
	}
	return request, data.Bytes()
}

func TestReadStreamCoalescesFileBurst(t *testing.T) {
	request, data := fileBurst(t, 128)
	input := &countedInput{Reader: bytes.NewReader(data)}
	var output capture
	ready := 0
	result, err := ReadStream(input, request, &output, func() { ready++ })
	if err != nil || ready != 1 || len(output.files) != 128 || result.Termination != trace.Expired {
		t.Fatal("coalesced stream changed its ordered observations or terminal result")
	}
	for _, event := range output.files {
		if event.Path.Reveal() != "/work/fixed-seed.bin" || *event.RequestedBytes != 4096 || *event.CompletedBytes != 128 {
			t.Fatal("buffered message changed meaning")
		}
	}
	const capacity = MaxMessageBytes + 4
	maximumReads := (len(data)+capacity-1)/capacity + 1 // final EOF read
	if input.calls > maximumReads || input.maximum > capacity {
		t.Fatalf("burst used %d reads (maximum %d), largest read %d (maximum %d)", input.calls, maximumReads, input.maximum, capacity)
	}
}

type fragmentedInput struct{ *bytes.Reader }

func (r fragmentedInput) Read(p []byte) (int, error) {
	return r.Reader.Read(p[:min(3, len(p))])
}

func TestReadStreamAcceptsFragmentedPipeMessages(t *testing.T) {
	request, data := fileBurst(t, 16)
	var output capture
	if _, err := ReadStream(fragmentedInput{bytes.NewReader(data)}, request, &output, func() {}); err != nil || len(output.files) != 16 {
		t.Fatal("transport fragmentation changed the stream")
	}
}

func TestBufferedTerminalStillRequiresActualPipeEOF(t *testing.T) {
	request, data := fileBurst(t, 1)
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	written := make(chan error, 1)
	go func() { _, err := writer.Write(data); written <- err }()
	var output capture
	_, err = ReadStream(reader, request, &output, func() {
		if err := reader.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
			t.Error(err)
			_ = reader.Close()
		}
	})
	if err != ErrProtocol || len(output.files) != 1 {
		t.Fatal("terminal bytes were mistaken for EOF on an open pipe")
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
}
