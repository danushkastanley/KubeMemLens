package incidentsession

import "time"

func validHeader(kind string, version int, opened, expires time.Time, closed *time.Time, count int) bool {
	retention := expires.Sub(opened)
	return kind == "IncidentSession" && (version == SchemaVersion || version == TraceExportSchema) && !opened.IsZero() &&
		retention >= time.Minute && retention <= 4*time.Hour && count >= 1 && count <= MaxEntries &&
		(closed == nil || !closed.IsZero()) && (closed != nil || count < MaxEntries)
}

func validLifecycle(kind Kind, index, count int, recorded time.Time, observed *time.Time, source string, refs int, note, gap string, closed *time.Time) bool {
	if source != "operator" || observed == nil || !observed.Equal(recorded) || refs != 0 || note != "" || gap != "" {
		return false
	}
	if kind == Opened {
		return index == 0
	}
	return kind == Closed && index == count-1 && closed != nil && closed.Equal(recorded)
}

func validateAuthorised(d AuthorisedExport) error {
	if d.Redacted || !digest(d.ID, 16) || !validHeader(d.Kind, d.SchemaVersion, d.OpenedAt, d.ExpiresAt, d.ClosedAt, len(d.Entries)) {
		return ErrInvalid
	}
	first := d.Entries[0]
	p := Principal{first.Namespace, first.NamespaceUID, first.Actor}
	if !p.valid() || first.Kind != Opened || !first.RecordedAt.Equal(d.OpenedAt) ||
		(d.ClosedAt != nil) != (d.Entries[len(d.Entries)-1].Kind == Closed) || !consistentReferences(d.Entries) ||
		d.SchemaVersion != exportSchema(d.Entries) || !consistentTraceReferences(d.Entries) {
		return ErrInvalid
	}
	c := chronology{opened: d.OpenedAt, expires: d.ExpiresAt}
	for i, e := range d.Entries {
		if e.Sequence != uint64(i+1) || e.RecordedAt.IsZero() || (e.ObservedAt != nil && e.ObservedAt.IsZero()) || e.Sensitivity != "private" ||
			(Principal{e.Namespace, e.NamespaceUID, e.Actor}) != p || (c.observe(privatePoint(e)) && !e.ClockUncertain) {
			return ErrInvalid
		}
		if e.Kind == Opened || e.Kind == Closed {
			if e.TraceReference != nil || !validLifecycle(e.Kind, i, len(d.Entries), e.RecordedAt, e.ObservedAt, e.Source, len(e.References), e.Note, e.GapReason, d.ClosedAt) {
				return ErrInvalid
			}
			continue
		}
		in := Input{Kind: e.Kind, Source: e.Source, Note: e.Note, References: e.References, GapReason: e.GapReason, traceReference: e.TraceReference}
		if e.ObservedAt != nil {
			in.ObservedAt = *e.ObservedAt
		}
		if e.Kind == Annotated {
			if e.ObservedAt == nil || !e.ObservedAt.Equal(e.RecordedAt) {
				return ErrInvalid
			}
			in.ObservedAt = time.Time{}
		}
		if in.validate(p) != nil {
			return ErrInvalid
		}
	}
	return validateCaptures(d, p)
}
