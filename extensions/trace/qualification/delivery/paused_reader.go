package delivery

import (
	"context"
	"time"
)

const readerPause = 5 * time.Second

// ReaderPause records a deliberate application read pause. Kernel and HTTP
// buffers may still accept bytes; this alone does not prove producer backpressure.
type ReaderPause struct {
	RequestedNanos  int64 `json:"requestedNanos"`
	StartedUnixNano int64 `json:"startedUnixNano"`
	EndedUnixNano   int64 `json:"endedUnixNano"`
	ElapsedNanos    int64 `json:"elapsedNanos"`
	Completed       bool  `json:"completed"`
}

type PausedResult struct {
	Pause       ReaderPause   `json:"pause"`
	Observation CeilingResult `json:"observation"`
}

// ConnectPausedReaderReady checks the exact active admission and metadata,
// announces readiness, and pauses application reads for five seconds before
// resuming the ceiling parser. The controller must independently prove resource
// bounds, loss/backpressure and cleanup, including incomplete stream outcomes.
func ConnectPausedReaderReady(ctx context.Context, connection Connection, expected Expectation, ready func() error) (PausedResult, error) {
	var result PausedResult
	if ready == nil {
		return result, ErrObservation
	}
	var err error
	result.Observation, err = connectCeiling(ctx, connection, expected, func() error {
		var pauseErr error
		result.Pause, pauseErr = pauseReads(ctx, ready)
		return pauseErr
	})
	return result, err
}

func pauseReads(ctx context.Context, ready func() error) (result ReaderPause, err error) {
	if ctx.Err() != nil {
		return result, ErrObservation
	}
	started := time.Now()
	result.RequestedNanos = int64(readerPause)
	result.StartedUnixNano = started.UnixNano()
	defer func() {
		ended := time.Now()
		result.EndedUnixNano = ended.UnixNano()
		result.ElapsedNanos = ended.Sub(started).Nanoseconds()
	}()
	timer := time.NewTimer(readerPause)
	defer timer.Stop()
	if ready() != nil {
		return result, ErrObservation
	}
	select {
	case <-ctx.Done():
		return result, ErrObservation
	case <-timer.C:
		if ctx.Err() != nil {
			return result, ErrObservation
		}
		result.Completed = true
		return result, nil
	}
}
