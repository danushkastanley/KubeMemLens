package workeripc

import (
	"encoding/binary"
	"os"
	"testing"
)

// Real OS pipes and one write per protocol message reproduce the worker's
// delivery pattern. This isolates IPC; it is not end-to-end qualification.
func BenchmarkReadStreamPipeBurst(b *testing.B) {
	request, data := fileBurst(b, 128)
	var messages [][]byte
	for offset := 0; offset < len(data); {
		end := offset + 4 + int(binary.BigEndian.Uint32(data[offset:]))
		messages = append(messages, data[offset:end])
		offset = end
	}
	reads := 0
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for range b.N {
		reader, writer, err := os.Pipe()
		if err != nil {
			b.Fatal(err)
		}
		written := make(chan error, 1)
		go func() {
			for _, message := range messages {
				if _, err := writer.Write(message); err != nil {
					_ = writer.Close()
					written <- err
					return
				}
			}
			written <- writer.Close()
		}()
		input := &countedInput{Reader: reader}
		var output capture
		_, readErr := ReadStream(input, request, &output, func() {})
		closeErr, writeErr := reader.Close(), <-written
		if readErr != nil || closeErr != nil || writeErr != nil || len(output.files) != 128 {
			b.Fatal("OS pipe benchmark did not deliver the complete valid stream")
		}
		reads += input.calls
	}
	b.ReportMetric(float64(reads)/float64(b.N), "reads/burst")
}
