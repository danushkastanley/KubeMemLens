package memoryhistory

import "time"

// NewReport validates the bounded query and creates an explicit missing grid.
// Adapters fill only samples
// whose identity, timestamp and metric meaning they have verified.
func NewReport(s Selection, q Query, now time.Time) (Report, error) {
	if err := s.Validate(); err != nil {
		return Report{}, err
	}
	if err := q.Validate(now); err != nil {
		return Report{}, err
	}
	r := Report{SchemaVersion: 1, Selection: s, Query: q, ReceivedAt: now, State: Missing, Completeness: Partial, Series: make([]Series, 0, len(s.Targets))}
	for _, target := range s.Targets {
		series := Series{FreshFor: SampleMaxAge, Target: target, Source: q.Source, Metric: q.Metric, Resolution: q.Step, Freshness: Missing, Completeness: Partial}
		for at := q.Start; !at.After(q.End); at = at.Add(q.Step) {
			series.Points = append(series.Points, Point{At: at, State: Missing})
		}
		r.Series = append(r.Series, series)
	}
	return r, nil
}

// Summarise separates current freshness from completeness across the window.
// A historical point is fresh relative to its own evaluation time.
func Summarise(r *Report, providerPartial bool) {
	r.State, r.Completeness = Missing, Complete
	for i := range r.Series {
		s := &r.Series[i]
		s.Freshness, s.Completeness = Missing, Complete
		for _, p := range s.Points {
			if p.State != Fresh {
				s.Completeness = Partial
			}
			if p.Bytes != nil {
				s.Freshness = Stale
				if r.ReceivedAt.Sub(p.SampledAt) <= s.FreshFor {
					s.Freshness = Fresh
				}
			}
		}
		if providerPartial {
			s.Completeness = Partial
		}
		if s.Completeness != Complete {
			r.Completeness = Partial
		}
		if s.Freshness == Fresh || (s.Freshness == Stale && r.State == Missing) {
			r.State = s.Freshness
		}
	}
}
