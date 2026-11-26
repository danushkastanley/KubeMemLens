package delivery

import "time"

// ClockDiagnostic retains elapsed clock readings only after qualification has
// already failed. The one follow-up reading never retries or replaces a frame.
// It helps distinguish a persistent offset from one isolated inconsistent read;
// neither outcome establishes why the clocks disagreed.
type ClockDiagnostic struct {
	RejectedWallElapsedNanos      int64 `json:"rejectedWallElapsedNanos"`
	RejectedMonotonicElapsedNanos int64 `json:"rejectedMonotonicElapsedNanos"`
	FollowUpWallElapsedNanos      int64 `json:"followUpWallElapsedNanos"`
	FollowUpMonotonicElapsedNanos int64 `json:"followUpMonotonicElapsedNanos"`
}

func clockDiagnostic(started, rejected, followUp time.Time) *ClockDiagnostic {
	return &ClockDiagnostic{
		RejectedWallElapsedNanos:      rejected.UnixNano() - started.UnixNano(),
		RejectedMonotonicElapsedNanos: rejected.Sub(started).Nanoseconds(),
		FollowUpWallElapsedNanos:      followUp.UnixNano() - started.UnixNano(),
		FollowUpMonotonicElapsedNanos: followUp.Sub(started).Nanoseconds(),
	}
}
