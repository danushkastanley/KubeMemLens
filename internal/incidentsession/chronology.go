package incidentsession

import "time"

type chronology struct {
	opened, expires, previous time.Time
	sources                   map[string]time.Time
}

type chronologyPoint struct {
	source, gap string
	recorded    time.Time
	observed    *time.Time
	references  []time.Time
}

func (c *chronology) observe(p chronologyPoint) bool {
	uncertain := p.gap == "clock-uncertain" || p.recorded.Before(c.opened) || !p.recorded.Before(c.expires)
	uncertain = uncertain || (!c.previous.IsZero() && p.recorded.Before(c.previous))
	c.previous = p.recorded
	if p.observed != nil {
		previous, known := c.sources[p.source]
		uncertain = uncertain || p.observed.After(p.recorded) || (known && p.observed.Before(previous))
		if c.sources == nil {
			c.sources = make(map[string]time.Time)
		}
		c.sources[p.source] = *p.observed
	}
	for _, at := range p.references {
		uncertain = uncertain || at.After(p.recorded)
	}
	return uncertain
}

func privatePoint(e Entry) chronologyPoint {
	p := chronologyPoint{source: e.Source, gap: e.GapReason, recorded: e.RecordedAt, observed: e.ObservedAt}
	for _, ref := range e.References {
		p.references = append(p.references, ref.ObservedAt)
	}
	return p
}

func publicPoint(e SanitisedEntry) chronologyPoint {
	p := chronologyPoint{source: e.Source, gap: e.GapReason, recorded: e.RecordedAt, observed: e.ObservedAt}
	for _, ref := range e.References {
		p.references = append(p.references, ref.ObservedAt)
	}
	return p
}

func entryClockRisk(r *record, e Entry) bool {
	c := chronology{opened: r.opened.UTC(), expires: r.expires.UTC()}
	for _, previous := range r.entries {
		c.observe(privatePoint(previous))
	}
	return c.observe(privatePoint(e))
}

// A digest identifies immutable capture bytes. A second timestamp for the same
// schema/digest is contradictory provenance, not a source-clock caveat.
func consistentReferences(entries []Entry) bool {
	type key struct {
		schema int
		digest string
	}
	seen := map[key]time.Time{}
	for _, e := range entries {
		for _, ref := range e.References {
			k := key{ref.SchemaVersion, ref.Digest}
			if previous, ok := seen[k]; ok && !previous.Equal(ref.ObservedAt) {
				return false
			}
			seen[k] = ref.ObservedAt
		}
	}
	return true
}
