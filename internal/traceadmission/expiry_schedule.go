package traceadmission

import "time"

func (m *Manager) wakeExpiry() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// nextExpiry is called with mu held. Pending/active entries need no polling
// before their deadline. Unfinished cleanup retains the existing retry cadence.
func (m *Manager) nextExpiry(now time.Time) time.Time {
	var next time.Time
	for _, e := range m.entries {
		at := e.expires
		if e.stage == closing {
			at = e.retryCleanup
			if !at.After(now) {
				at = now.Add(100 * time.Millisecond)
			}
		}
		if next.IsZero() || at.Before(next) {
			next = at
		}
	}
	return next
}
