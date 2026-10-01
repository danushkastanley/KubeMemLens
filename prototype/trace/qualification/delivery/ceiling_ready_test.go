package delivery

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func TestCeilingReadinessPrecedesControlledProducerBurst(t *testing.T) {
	data, expected, deadline, now := ceilingFixture(t, trace.EventLimit, 1)
	frames := bytes.SplitAfter(data, []byte("\n"))
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	ready, finished := make(chan struct{}), make(chan error, 1)
	go func() {
		if _, err := writer.Write(frames[0]); err != nil {
			finished <- err
			return
		}
		select {
		case <-ready:
		case <-ctx.Done():
			_ = writer.CloseWithError(ctx.Err())
			finished <- ctx.Err()
			return
		}
		_, err := writer.Write(bytes.Join(frames[1:], nil))
		_ = writer.Close()
		finished <- err
	}()
	activeChecked := false
	result, err := observeCeiling(reader, expected, now, func() (time.Time, error) {
		activeChecked = true
		return deadline, nil
	}, func() error {
		if !activeChecked {
			return errors.New("active admission not checked")
		}
		close(ready)
		return nil
	})
	if err != nil || !result.CeilingReported || !result.TransportComplete {
		t.Fatal(result, err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}

func TestUnmatchedOrUnwritableReadinessCannotYieldCeilingEvidence(t *testing.T) {
	for _, mode := range []string{"identity", "active-deadline", "write-failed"} {
		data, expected, deadline, now := ceilingFixture(t, trace.EventLimit, 1)
		if mode == "identity" {
			expected.SessionID = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
		}
		if mode == "active-deadline" {
			deadline = deadline.Add(-time.Second)
		}
		announcements := 0
		result, err := observeCeiling(bytes.NewReader(data), expected, now,
			func() (time.Time, error) { return deadline, nil }, func() error {
				announcements++
				return io.ErrClosedPipe
			})
		if err == nil || result.TransportComplete || (mode != "write-failed" && announcements != 0) || (mode == "write-failed" && announcements != 1) {
			t.Fatal("unproven readiness yielded evidence", mode, result, err)
		}
	}
}
