package sdk

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

// File programmes commit without notification. A timed read drains even a
// single queued record; target validation may require an earlier deadline.
func ringReadDeadline(kind trace.Kind, now, validation time.Time) time.Time {
	if kind == trace.Files {
		poll := now.Add(10 * time.Millisecond)
		if poll.Before(validation) {
			return poll
		}
	}
	return validation
}
