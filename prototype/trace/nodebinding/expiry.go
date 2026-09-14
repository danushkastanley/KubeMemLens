package nodebinding

import (
	"context"
	"time"
)

// prune is called with mu held. Expiry never depends on a controller request.
func (s *Service) prune(now time.Time) {
	for id, l := range s.leases {
		if !now.Before(l.expires) {
			delete(s.leases, id)
			s.closeHandle(l.handle)
		}
	}
	for id, expires := range s.seen {
		if !now.Before(expires) {
			delete(s.seen, id)
		}
	}
}
func (s *Service) expire(ctx context.Context) {
	defer close(s.done)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.mu.Lock()
			s.prune(time.Now())
			s.mu.Unlock()
		case <-ctx.Done():
			s.mu.Lock()
			s.closed = true
			for id, l := range s.leases {
				delete(s.leases, id)
				s.closeHandle(l.handle)
			}
			clear(s.seen)
			s.mu.Unlock()
			return
		}
	}
}
func (s *Service) Close(ctx context.Context) error {
	s.cancel()
	select {
	case <-s.done:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.cleanupErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
