package memoryhistory

import (
	"net/url"
	"strconv"
	"time"
)

// ParseQuery owns default windows and metrics for both HTTP and terminal clients.
// Explicit source selection is required to change measurement semantics.
func ParseQuery(values url.Values, scope Scope, now time.Time) (Query, error) {
	for key, v := range values {
		switch key {
		case "source", "metric", "start", "end", "step", "container", "kind":
		default:
			return Query{}, ErrInvalid
		}
		if len(v) != 1 || len(v[0]) > 128 {
			return Query{}, ErrInvalid
		}
	}
	q := Query{Source: Local, Metric: Charge, End: now.UTC().Truncate(time.Second), Step: time.Minute}
	if source := values.Get("source"); source != "" {
		q.Source = Source(source)
	}
	window := 15 * time.Minute
	if q.Source == Prometheus {
		window = 24 * time.Hour
	}
	if q.Source == Prometheus || scope == Node {
		q.Metric = WorkingSet
	}
	if metric := values.Get("metric"); metric != "" {
		q.Metric = Metric(metric)
	}
	var err error
	if value := values.Get("end"); value != "" {
		q.End, err = time.Parse(time.RFC3339, value)
		if err != nil {
			return Query{}, ErrInvalid
		}
	}
	q.Start = q.End.Add(-window)
	if value := values.Get("start"); value != "" {
		q.Start, err = time.Parse(time.RFC3339, value)
		if err != nil {
			return Query{}, ErrInvalid
		}
	}
	span := q.End.Sub(q.Start)
	if span < 0 {
		return Query{}, ErrInvalid
	}
	if span > MaxRange {
		return Query{}, ErrBounds
	}
	minimumStep := (span + time.Duration(MaxPoints-2)) / time.Duration(MaxPoints-1)
	q.Step = max(MinStep, (minimumStep+time.Minute-1)/time.Minute*time.Minute)
	if value := values.Get("step"); value != "" {
		seconds, err := strconv.ParseInt(value, 10, 32)
		if err != nil || seconds < 60 || seconds > 3600 {
			return Query{}, ErrBounds
		}
		q.Step = time.Duration(seconds) * time.Second
	}
	return q, q.Validate(now)
}
