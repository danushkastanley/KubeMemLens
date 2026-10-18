package workeripc

import (
	"bufio"
	"encoding/binary"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

type fileBatch struct {
	output trace.Output
	events [trace.MaxFileBatch]trace.FileActivity
	count  int
}

func (b *fileBatch) clear() {
	clear(b.events[:])
	b.count = 0
}

func (b *fileBatch) flush() error {
	if b.count == 0 {
		return nil
	}
	defer b.clear()
	if output, ok := b.output.(trace.FileBatchOutput); ok {
		return output.FileActivities(b.events[:b.count])
	}
	for _, event := range b.events[:b.count] {
		if err := b.output.FileActivity(event); err != nil {
			return err
		}
	}
	return nil
}

// No underlying read is allowed here: sparse observations must be delivered
// before waiting on an incomplete next message or an idle pipe.
func completeBufferedMessage(stream *bufio.Reader) bool {
	if stream.Buffered() < 4 {
		return false
	}
	header, _ := stream.Peek(4)
	size := binary.BigEndian.Uint32(header)
	return size > 0 && size <= MaxMessageBytes && int(size)+4 <= stream.Buffered()
}

func fileObservation(f *fileWire, spec trace.Specification) (trace.FileActivity, error) {
	path, err := trace.NewSensitiveText(f.Path, spec.Bounds().PathBytes)
	if err != nil {
		return trace.FileActivity{}, ErrProtocol
	}
	return trace.FileActivity{ObservedAt: f.ObservedAt, Operation: f.Operation, RequestedBytes: &f.Requested, CompletedBytes: &f.Completed, Path: path}, nil
}

func (b *fileBatch) add(event trace.FileActivity, stream *bufio.Reader) error {
	b.events[b.count] = event
	b.count++
	if b.count == len(b.events) || !completeBufferedMessage(stream) {
		return b.flush()
	}
	return nil
}
