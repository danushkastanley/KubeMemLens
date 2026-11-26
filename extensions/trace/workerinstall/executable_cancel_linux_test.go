package workerinstall

import (
	"context"
	"errors"
	"io"
	"runtime"
	"strings"
	"testing"
)

func TestExecutableRejectsCancelledContext(t *testing.T) {
	policy, path := acceptedSelf(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if image, err := policy.Executable(ctx, path, runtime.GOARCH); err == nil || image != nil {
		t.Fatal("cancelled installation retained executable")
	}
}

func TestExecutableReaderStopsBetweenCopyChunks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := executableReader{ctx, strings.NewReader(strings.Repeat("x", 128<<10))}
	chunk := make([]byte, 64<<10)
	if n, err := r.Read(chunk); n != len(chunk) || err != nil {
		t.Fatal("first copy chunk failed")
	}
	cancel()
	if n, err := io.Copy(io.Discard, r); n != 0 || !errors.Is(err, context.Canceled) {
		t.Fatal("copy continued after cancellation")
	}
}
