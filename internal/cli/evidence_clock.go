package cli

import (
	"fmt"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

// evidenceClock is the instant that ages and recency are measured against.
// A live read is measured against now. A replayed incident is measured against
// its capture time, so the same file gives the same answer whenever it is read.
type evidenceClock struct {
	at       time.Time
	relation string
}

func liveClock() evidenceClock {
	return evidenceClock{at: time.Now().UTC(), relation: "ago"}
}

// captureClock uses the incident's capture time, or the Pod's own sample time
// for bundles written without one. With neither, no age is claimed.
func captureClock(capturedAt time.Time, pod api.PodSnapshot) evidenceClock {
	if capturedAt.IsZero() {
		capturedAt = pod.CapturedAt
	}
	return evidenceClock{at: capturedAt.UTC(), relation: "before capture"}
}

// age describes how long before the clock a moment was, such as
// " (2m0s ago)", or nothing when the clock is unknown.
func (c evidenceClock) age(moment time.Time) string {
	if c.at.IsZero() {
		return ""
	}
	return fmt.Sprintf(" (%s %s)", max(c.at.Sub(moment), 0).Round(time.Second), c.relation)
}

// podEvidenceTime is the instant a Pod snapshot describes: its own sample
// time, or now for snapshots that carry none.
func podEvidenceTime(pod api.PodSnapshot) time.Time {
	if pod.CapturedAt.IsZero() {
		return time.Now().UTC()
	}
	return pod.CapturedAt.UTC()
}
