package incidentsession

import (
	"strconv"
	"time"
)

func validateSanitised(d SanitisedExport) error {
	if !d.Redacted || d.Alias != "session-1" || !validHeader(d.Kind, d.SchemaVersion, d.OpenedAt, d.ExpiresAt, d.ClosedAt, len(d.Entries)) {
		return ErrInvalid
	}
	if d.Entries[0].Kind != Opened || !d.Entries[0].RecordedAt.Equal(d.OpenedAt) ||
		(d.ClosedAt != nil) != (d.Entries[len(d.Entries)-1].Kind == Closed) {
		return ErrInvalid
	}
	c := chronology{opened: d.OpenedAt, expires: d.ExpiresAt}
	aliases := map[string]SanitisedReference{}
	type traceAliasState struct {
		reference  SanitisedTraceReference
		capturedAt time.Time
	}
	traceAliases := map[string]traceAliasState{}
	for i, e := range d.Entries {
		if e.Sequence != uint64(i+1) || e.RecordedAt.IsZero() || (e.ObservedAt != nil && e.ObservedAt.IsZero()) || e.Namespace != "namespace-1" || e.Actor != "operator-1" ||
			e.Sensitivity != "redacted" || !validSource(e.Source) || (c.observe(publicPoint(e)) && !e.ClockUncertain) {
			return ErrInvalid
		}
		if e.Kind == Opened || e.Kind == Closed {
			if e.TraceReference != nil || !validLifecycle(e.Kind, i, len(d.Entries), e.RecordedAt, e.ObservedAt, e.Source, len(e.References), "", e.GapReason, d.ClosedAt) {
				return ErrInvalid
			}
			continue
		}
		if validSanitisedPayload(e) != nil {
			return ErrInvalid
		}
		if e.TraceReference != nil {
			ref := *e.TraceReference
			previous, seen := traceAliases[ref.Alias]
			if (seen && (previous.reference != ref || !previous.capturedAt.Equal(*e.ObservedAt))) || (!seen && ref.Alias != "trace-"+strconv.Itoa(len(traceAliases)+1)) {
				return ErrInvalid
			}
			traceAliases[ref.Alias] = traceAliasState{ref, *e.ObservedAt}
		}
		for _, ref := range e.References {
			if ref.SchemaVersion < 1 || ref.SchemaVersion > 7 || ref.ObservedAt.IsZero() {
				return ErrInvalid
			}
			previous, seen := aliases[ref.Alias]
			if seen {
				if previous.SchemaVersion != ref.SchemaVersion || !previous.ObservedAt.Equal(ref.ObservedAt) {
					return ErrInvalid
				}
				continue
			}
			if ref.Alias != "evidence-"+strconv.Itoa(len(aliases)+1) {
				return ErrInvalid
			}
			aliases[ref.Alias] = ref
		}
	}
	if (len(traceAliases) != 0) != (d.SchemaVersion == TraceExportSchema) {
		return ErrInvalid
	}
	return nil
}

func validSanitisedPayload(e SanitisedEntry) error {
	if e.Kind != Gap && e.GapReason != "" {
		return ErrInvalid
	}
	if (e.Kind == TraceReferenced) != (e.TraceReference != nil) {
		return ErrInvalid
	}
	switch e.Kind {
	case TraceReferenced:
		if e.Source != traceReferenceSource || len(e.References) != 0 || e.ObservedAt == nil || e.TraceReference.validate() != nil {
			return ErrInvalid
		}
	case Annotated:
		if e.Source != "operator" || len(e.References) != 0 || e.ObservedAt == nil || !e.ObservedAt.Equal(e.RecordedAt) {
			return ErrInvalid
		}
	case Captured, Marked:
		if len(e.References) != 1 || e.ObservedAt == nil {
			return ErrInvalid
		}
		if e.Kind == Marked && e.References[0].SchemaVersion != 6 {
			return ErrInvalid
		}
	case Compared:
		if len(e.References) != 2 || e.ObservedAt == nil || !compatibleCaptureSchemas(e.References[0].SchemaVersion, e.References[1].SchemaVersion) {
			return ErrInvalid
		}
	case Gap:
		if len(e.References) != 0 || !validGap(e.GapReason) {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}
