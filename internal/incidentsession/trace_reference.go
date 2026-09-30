package incidentsession

import (
	"context"
	"strconv"

	"github.com/danushkastanley/kube-memlens/internal/tracereport"
)

const traceReferenceSource = "operator-trace-report"

// RecordTraceReference records an explicitly operator-supplied reference. The
// submitting actor is authenticated; file agreement and source authenticity are
// not established by this server. Raw report bytes never cross this boundary.
func (s *Store) RecordTraceReference(ctx context.Context, p Principal, id string, ref tracereport.Reference) (Summary, error) {
	if err := s.authorize(ctx, p, ReferenceTrace, id); err != nil {
		return Summary{}, err
	}
	if ref.Validate() != nil {
		return Summary{}, ErrInvalid
	}
	input := Input{Kind: TraceReferenced, Source: traceReferenceSource, ObservedAt: ref.CapturedAt, traceReference: &ref}
	if input.validate(p) != nil {
		return Summary{}, ErrInvalid
	}
	return s.mutate(ctx, p, id, input)
}

// Export v1 remains byte-compatible for timelines without trace references.
// The v2 representation is required as soon as a trace entry is present.
func exportSchema(entries []Entry) int {
	for _, entry := range entries {
		if entry.Kind == TraceReferenced {
			return TraceExportSchema
		}
	}
	return SchemaVersion
}

func consistentTraceReferences(entries []Entry) bool {
	seen := map[string]tracereport.Reference{}
	for _, entry := range entries {
		if entry.TraceReference == nil {
			continue
		}
		ref := *entry.TraceReference
		ref.CapturedAt = ref.CapturedAt.UTC()
		if previous, ok := seen[ref.Digest]; ok && previous != ref {
			return false
		}
		seen[ref.Digest] = ref
	}
	return true
}

type SanitisedTraceReference struct {
	Alias               string `json:"alias"`
	Provenance          string `json:"provenance"`
	ReportSchemaVersion int    `json:"reportSchemaVersion"`
	tracereport.ReferenceOutcome
}

func sanitiseTraceReference(aliases map[string]string, ref tracereport.Reference) SanitisedTraceReference {
	alias := traceAlias(aliases, ref.Digest)
	return SanitisedTraceReference{alias, ref.Provenance, ref.ReportSchemaVersion, ref.Outcome()}
}

func (r SanitisedTraceReference) validate() error {
	if r.Provenance != tracereport.OperatorSupplied || r.ReportSchemaVersion < tracereport.MinimumSchema || r.ReportSchemaVersion > tracereport.CurrentSchema || r.ReferenceOutcome.Validate() != nil {
		return ErrInvalid
	}
	return nil
}

func traceAlias(aliases map[string]string, digest string) string {
	alias, exists := aliases[digest]
	if !exists {
		alias = "trace-" + strconv.Itoa(len(aliases)+1)
		aliases[digest] = alias
	}
	return alias
}

// SelectTraceReference returns a value for offline file verification. It never
// fetches a file, trusts its origin or restores a live session.
func SelectTraceReference(document AuthorisedExport, selector string) (tracereport.Reference, error) {
	if validateAuthorised(document) != nil {
		return tracereport.Reference{}, ErrInvalid
	}
	aliases := map[string]string{}
	for _, entry := range document.Entries {
		if entry.TraceReference == nil {
			continue
		}
		ref := *entry.TraceReference
		if selector == traceAlias(aliases, ref.Digest) || selector == ref.Digest {
			return ref, nil
		}
	}
	return tracereport.Reference{}, ErrNotFound
}
