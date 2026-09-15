//go:build linux && (amd64 || arm64)

package main

import (
	"errors"
	"os"
	"testing"
	"time"
)

func TestInheritedPipeReadDeadline(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	input, err := pollablePipe(reader)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if input.SetReadDeadline(time.Now().Add(20*time.Millisecond)) != nil {
		t.Fatal("deadline unsupported")
	}
	var data [1]byte
	if _, err := input.Read(data[:]); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatal("stalled inherited pipe did not time out")
	}
}

func TestBlockedOutputRespectsWriteBound(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	output, err := pollablePipe(writer)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	start := time.Now()
	_, err = (pipeWriter{output}).Write(make([]byte, 2<<20))
	if !errors.Is(err, os.ErrDeadlineExceeded) || time.Since(start) > 3*time.Second {
		t.Fatal("blocked output escaped write deadline")
	}
}

func TestRegularFileCannotReplaceProtocolPipe(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "regular")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if pipe, err := pollablePipe(file); err == nil || pipe != nil {
		t.Fatal("regular protocol file accepted")
	}
}
