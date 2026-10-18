package delivery

import (
	"bytes"
	"context"
	"io"
	"testing"
	"testing/synctest"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func TestCeilingParserDoesNotReadEventsDuringPause(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data, expected, deadline, now := ceilingFixture(t, trace.EventLimit, 1)
		frames := bytes.SplitAfter(data, []byte("\n"))
		reader, writer := io.Pipe()
		defer reader.Close()
		defer writer.Close()
		ready := make(chan struct{})
		finished := make(chan error, 1)
		started := time.Now()
		go func() {
			if _, err := writer.Write(frames[0]); err != nil {
				finished <- err
				return
			}
			<-ready
			_, err := writer.Write(bytes.Join(frames[1:], nil))
			if time.Since(started) < readerPause {
				t.Error("reader consumed post-metadata frames before pause ended")
			}
			_ = writer.Close()
			finished <- err
		}()
		result, err := observeCeiling(reader, expected, now, func() (time.Time, error) {
			return deadline, nil
		}, func() error {
			_, err := pauseReads(t.Context(), func() error { close(ready); return nil })
			return err
		})
		if err != nil || !result.CeilingReported || !result.TransportComplete {
			t.Fatal(result, err)
		}
		if err := <-finished; err != nil {
			t.Fatal(err)
		}
	})
}

func TestReaderPauseAnnouncesBeforeWaitingAndRetainsDuration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := time.Now()
		announced := false
		result, err := pauseReads(t.Context(), func() error {
			announced = true
			if !time.Now().Equal(started) {
				t.Fatal("pause preceded the controller readiness signal")
			}
			return nil
		})
		if err != nil || !announced || !result.Completed || result.RequestedNanos != int64(5*time.Second) || result.ElapsedNanos != int64(5*time.Second) {
			t.Fatal(result, err)
		}
		if result.EndedUnixNano-result.StartedUnixNano != result.ElapsedNanos {
			t.Fatal("pause timestamps do not match elapsed time")
		}
	})
}

func TestReaderPauseCancellationAndFailedReadinessStayIncomplete(t *testing.T) {
	for _, stage := range []string{"before", "during", "ready-write"} {
		t.Run(stage, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if stage == "before" {
					cancel()
				}
				announcements := 0
				result, err := pauseReads(ctx, func() error {
					announcements++
					if stage == "ready-write" {
						return io.ErrClosedPipe
					}
					time.AfterFunc(time.Second, cancel)
					return nil
				})
				if err == nil || result.Completed || (stage == "before" && announcements != 0) {
					t.Fatal("failed pause yielded completion", result, err)
				}
				if stage == "during" && result.ElapsedNanos != int64(time.Second) {
					t.Fatal("cancelled pause did not stop immediately", result)
				}
			})
		})
	}
}
