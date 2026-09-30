package incidentsession

import "context"

// Inspect returns only an owned session's bounded lifecycle metadata.
func (s *Store) Inspect(ctx context.Context, p Principal, id string) (Summary, error) {
	if err := s.authorize(ctx, p, Inspect, id); err != nil {
		return Summary{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.available(ctx); err != nil {
		return Summary{}, err
	}
	r, err := s.find(p, id)
	if err != nil {
		return Summary{}, err
	}
	return r.summary(), nil
}
