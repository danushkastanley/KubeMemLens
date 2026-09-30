package incidentsession

import (
	"context"
	"encoding/json"
	"time"
)

type AuthorisedExport struct {
	Kind          string             `json:"kind"`
	SchemaVersion int                `json:"schemaVersion"`
	Redacted      bool               `json:"redacted"`
	ID            string             `json:"id"`
	OpenedAt      time.Time          `json:"openedAt"`
	ExpiresAt     time.Time          `json:"expiresAt"`
	ClosedAt      *time.Time         `json:"closedAt,omitempty"`
	Entries       []Entry            `json:"entries"`
	Captures      []CapturedEvidence `json:"captures,omitempty"`
	LimitReached  bool               `json:"limitReached"`
}

func fullDocument(r *record) AuthorisedExport {
	return AuthorisedExport{Kind: "IncidentSession", SchemaVersion: exportSchema(r.entries), ID: r.id,
		OpenedAt: r.opened.UTC(), ExpiresAt: r.expires.UTC(), ClosedAt: r.closed, Entries: r.entries, Captures: r.captures, LimitReached: r.limitReached}
}

// Export is an explicit authorised boundary, not a generic JSON/logging method.
// Sanitised output projects an allow-list, never edits private fields in place.
func (s *Store) Export(ctx context.Context, p Principal, id string, operation Operation) ([]byte, error) {
	if operation != ExportSanitised && operation != ExportAuthorised {
		return nil, ErrInvalid
	}
	if err := s.authorize(ctx, p, operation, id); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	r, err := s.find(p, id)
	if err != nil {
		return nil, err
	}
	var output any = fullDocument(r)
	if operation == ExportSanitised {
		output = sanitisedDocument(r)
	}
	data, err := json.Marshal(output)
	if err != nil || len(data) > s.limits.SessionBytes {
		return nil, ErrCapacity
	}
	if err := s.available(ctx); err != nil {
		return nil, err
	}
	return data, nil
}

// Separate types deliberately have no fields for private identities, digests or
// notes. New private fields cannot silently become part of this public contract.
type SanitisedReference struct {
	Alias         string    `json:"alias"`
	SchemaVersion int       `json:"schemaVersion"`
	ObservedAt    time.Time `json:"observedAt"`
}

type SanitisedEntry struct {
	Sequence       uint64                   `json:"sequence"`
	Kind           Kind                     `json:"kind"`
	RecordedAt     time.Time                `json:"recordedAt"`
	ObservedAt     *time.Time               `json:"observedAt"`
	Namespace      string                   `json:"namespace"`
	Actor          string                   `json:"actor"`
	Source         string                   `json:"source"`
	Sensitivity    string                   `json:"sensitivity"`
	ClockUncertain bool                     `json:"clockUncertain"`
	References     []SanitisedReference     `json:"references,omitempty"`
	GapReason      string                   `json:"gapReason,omitempty"`
	TraceReference *SanitisedTraceReference `json:"traceReference,omitempty"`
}

type SanitisedExport struct {
	Kind          string           `json:"kind"`
	SchemaVersion int              `json:"schemaVersion"`
	Redacted      bool             `json:"redacted"`
	Alias         string           `json:"alias"`
	OpenedAt      time.Time        `json:"openedAt"`
	ExpiresAt     time.Time        `json:"expiresAt"`
	ClosedAt      *time.Time       `json:"closedAt,omitempty"`
	Entries       []SanitisedEntry `json:"entries"`
	LimitReached  bool             `json:"limitReached"`
}

func sanitisedDocument(r *record) SanitisedExport {
	d := SanitisedExport{Kind: "IncidentSession", SchemaVersion: exportSchema(r.entries), Redacted: true,
		Alias: "session-1", OpenedAt: r.opened.UTC(), ExpiresAt: r.expires.UTC(), ClosedAt: copyTimestamp(r.closed), LimitReached: r.limitReached}
	aliases := map[string]string{}
	traceAliases := map[string]string{}
	for _, e := range r.entries {
		visible := SanitisedEntry{Sequence: e.Sequence, Kind: e.Kind, RecordedAt: e.RecordedAt,
			ObservedAt: copyTimestamp(e.ObservedAt), Namespace: "namespace-1", Actor: "operator-1", Source: e.Source,
			Sensitivity: "redacted", ClockUncertain: e.ClockUncertain, GapReason: e.GapReason}
		for _, ref := range e.References {
			alias := referenceAlias(aliases, ref)
			visible.References = append(visible.References, SanitisedReference{alias, ref.SchemaVersion, ref.ObservedAt})
		}
		if e.TraceReference != nil {
			ref := sanitiseTraceReference(traceAliases, *e.TraceReference)
			visible.TraceReference = &ref
		}
		d.Entries = append(d.Entries, visible)
	}
	return d
}

// Sanitise projects a validated offline authorised export using the same
// allow-list as live export. It grants no authority to restore session state.
func Sanitise(document AuthorisedExport) (SanitisedExport, error) {
	if validateAuthorised(document) != nil {
		return SanitisedExport{}, ErrInvalid
	}
	first := document.Entries[0]
	record := record{principal: Principal{first.Namespace, first.NamespaceUID, first.Actor}, id: document.ID, opened: document.OpenedAt, expires: document.ExpiresAt, closed: document.ClosedAt, entries: document.Entries, limitReached: document.LimitReached}
	return sanitisedDocument(&record), nil
}

func copyTimestamp(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
