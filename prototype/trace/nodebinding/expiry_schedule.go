package nodebinding

import "time"

func (s *Service) wakeExpiry() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// nextExpiry is called with mu held. Active executions own deadline contexts;
// sweeping them must not race that timer by turning expiry into cancellation.
// Their references and replay records remain retained until finish notifies us.
func (s *Service) nextExpiry() time.Time {
	var next time.Time
	for _, l := range s.leases {
		if l.execution != nil {
			continue
		}
		at := l.expires
		if next.IsZero() || at.Before(next) {
			next = at
		}
	}
	for id, at := range s.seen {
		if _, busy := s.leases[id]; busy {
			continue
		}
		if next.IsZero() || at.Before(next) {
			next = at
		}
	}
	return next
}
