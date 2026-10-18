package memoryhistory

import "time"

// Validate protects clients from oversized, retargeted or semantically mixed
// responses. Conservative partial evidence is allowed; invented completeness is not.
func (r Report) Validate() error {
	if r.SchemaVersion != 1 || r.Selection.Validate() != nil || r.Query.Validate(r.ReceivedAt) != nil || r.ReceivedAt.Before(r.Selection.ResolvedAt) {
		return ErrInvalid
	}
	switch r.Reason {
	case "", "provider-partial", "series-unreported", "local-history-unreported", "metric-not-supported", "local-metric-not-retained", "local-node-source-disabled":
	default:
		return ErrInvalid
	}
	if r.State == Unsupported || r.State == Disabled {
		if r.State == Disabled && (r.Query.Source != Local || r.Selection.Request.Scope != Node || r.Reason != "local-node-source-disabled") {
			return ErrInvalid
		}
		if len(r.Series) != 0 || r.Completeness != Partial {
			return ErrInvalid
		}
		return nil
	}
	expected := map[Target]bool{}
	for _, target := range r.Selection.Targets {
		if r.Query.Source == Local && r.Selection.Request.Scope != Node {
			if r.Query.Metric != Charge || r.Selection.Request.Scope == Container || target.PodCreatedAt.IsZero() {
				return ErrInvalid
			}
			target.Container, target.ContainerID = "", ""
			target.StartedAt = target.PodCreatedAt
		}
		expected[target] = true
	}
	if len(r.Series) != len(expected) {
		return ErrInvalid
	}
	for _, s := range r.Series {
		if s.FreshFor <= 0 || s.FreshFor > SampleMaxAge || (s.Source == Prometheus && s.FreshFor != SampleMaxAge) {
			return ErrInvalid
		}
		if !expected[s.Target] || s.Source != r.Query.Source || s.Metric != r.Query.Metric || s.Resolution != r.Query.Step {
			return ErrInvalid
		}
		delete(expected, s.Target)
		origin, clock := "cadvisor", "prometheus-sample"
		if r.Query.Source == Local {
			origin, clock = "cgroup-v2", "collector-capture"
			if r.Selection.Request.Scope == Node {
				origin, clock = "kubelet-summary", "kubelet-sample"
			}
		}
		if s.Origin != origin || s.SampleClock != clock || len(s.Points) != int(r.Query.End.Sub(r.Query.Start)/r.Query.Step)+1 {
			return ErrInvalid
		}
		for i, p := range s.Points {
			if !p.At.Equal(r.Query.Start.Add(s.Resolution * time.Duration(i))) {
				return ErrInvalid
			}
			if p.State == Missing {
				if p.Bytes != nil || !p.SampledAt.IsZero() {
					return ErrInvalid
				}
				continue
			}
			if p.Bytes == nil || p.SampledAt.IsZero() || p.SampledAt.After(p.At) || p.SampledAt.Before(s.Target.StartedAt) {
				return ErrInvalid
			}
			state := Fresh
			if p.At.Sub(p.SampledAt) > s.FreshFor {
				state = Stale
			}
			if p.State != state {
				return ErrInvalid
			}
		}
	}
	check := r
	check.Series = append([]Series(nil), r.Series...)
	Summarise(&check, false)
	if r.State != check.State || !validCompleteness(r.Completeness) || (r.Completeness == Complete && check.Completeness != Complete) {
		return ErrInvalid
	}
	for i, s := range r.Series {
		if s.Freshness != check.Series[i].Freshness || !validCompleteness(s.Completeness) || (s.Completeness == Complete && check.Series[i].Completeness != Complete) {
			return ErrInvalid
		}
	}
	return nil
}

func validCompleteness(c Completeness) bool { return c == Complete || c == Partial }
