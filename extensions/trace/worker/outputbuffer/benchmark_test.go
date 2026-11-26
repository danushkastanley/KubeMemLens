package outputbuffer

import (
	"bytes"
	"io"
	"os"
	"testing"
)

type countedPipe struct {
	io.Writer
	calls int
}

func (p *countedPipe) Write(data []byte) (int, error) {
	p.calls++
	return p.Writer.Write(data)
}

func BenchmarkPipeBurst(b *testing.B) {
	for _, mode := range []string{"direct", "buffered"} {
		b.Run(mode, func(b *testing.B) {
			reader, output, err := os.Pipe()
			if err != nil {
				b.Fatal(err)
			}
			defer reader.Close()
			defer output.Close()
			done := make(chan error, 1)
			go func() { _, err := io.Copy(io.Discard, reader); done <- err }()
			pipe := &countedPipe{Writer: output}
			var writer io.Writer = pipe
			flush := func() error { return nil }
			var buffer *Writer
			if mode == "buffered" {
				buffer = New(pipe)
				defer buffer.Abort()
				writer, flush = buffer, buffer.Flush
			}
			data := bytes.Repeat([]byte("e"), 200)
			b.SetBytes(int64(128 * len(data)))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				for range 128 {
					if _, err := writer.Write(data); err != nil {
						b.Fatal(err)
					}
				}
				if err := flush(); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			if buffer != nil {
				buffer.Abort()
			}
			b.ReportMetric(float64(pipe.calls)/float64(b.N), "writes/burst")
			if err := output.Close(); err != nil {
				b.Fatal(err)
			}
			if err := <-done; err != nil {
				b.Fatal(err)
			}
		})
	}
}
