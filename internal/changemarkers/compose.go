package changemarkers

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

// Compose retains the most recent bounded evidence, with truncation explicit.
// A marker's source time is never replaced by a memory sample's query-grid time.
func Compose(selection memoryhistory.Selection, query memoryhistory.Query, observed time.Time, coverage Coverage, candidates []Marker, truncated bool) (Report, error) {
	r := Report{SchemaVersion: 1, Request: selection.Request, UID: selection.UID, Start: query.Start, End: query.End, ObservedAt: observed, Events: coverage, Truncated: truncated}
	if selection.Validate() != nil || query.Validate(observed) != nil || observed.Before(selection.ResolvedAt) {
		return Report{}, ErrInvalid
	}
	if len(candidates) > MaxEvents+memoryhistory.MaxTargets*8 {
		return Report{}, ErrBounds
	}
	seen := map[string]bool{}
	for _, marker := range candidates {
		if marker.Validate() != nil || marker.Until.After(observed) {
			return Report{}, ErrInvalid
		}
		if marker.Subject.Namespace != selection.Request.Namespace || !r.includes(marker) {
			return Report{}, ErrInvalid
		}
		if marker.Kind != Revision && (marker.Until.Before(query.Start) || marker.At.After(query.End)) {
			continue
		}
		key := markerKey(marker)
		if seen[key] {
			return Report{}, ErrInvalid
		}
		seen[key] = true
		marker.Owners = append([]Object(nil), marker.Owners...)
		r.Markers = append(r.Markers, marker)
	}
	slices.SortFunc(r.Markers, compareMarker)
	if len(r.Markers) > MaxMarkers {
		r.Truncated = true
		r.Markers = append([]Marker(nil), r.Markers[len(r.Markers)-MaxMarkers:]...)
	}
	return r, r.Validate()
}

// Aligned identifies the grid buckets intersected by a source event/interval.
// An observation outside the query remains unaligned (including current revision
// context). The original Marker still carries the exact, unclamped source times.
type Aligned struct {
	Marker      Marker
	First, Last int
}

func Align(report Report, query memoryhistory.Query) ([]Aligned, error) {
	if report.Validate() != nil || query.Validate(report.ObservedAt) != nil || !report.Start.Equal(query.Start) || !report.End.Equal(query.End) {
		return nil, ErrInvalid
	}
	result := make([]Aligned, 0, len(report.Markers))
	for _, marker := range report.Markers {
		first, last := -1, -1
		if marker.Kind != Revision && !marker.At.After(query.End) && !marker.Until.Before(query.Start) {
			start, end := marker.At, marker.Until
			if start.Before(query.Start) {
				start = query.Start
			}
			if end.After(query.End) {
				end = query.End
			}
			first, last = int(start.Sub(query.Start)/query.Step), int(end.Sub(query.Start)/query.Step)
		}
		marker.Owners = append([]Object(nil), marker.Owners...)
		result = append(result, Aligned{marker, first, last})
	}
	return result, nil
}

func markerKey(m Marker) string {
	return strings.Join([]string{string(m.Clock), string(m.Kind), m.SourceUID, m.Subject.UID, m.Container, m.ResizeState}, "\x00")
}

func compareMarker(a, b Marker) int {
	if n := a.At.Compare(b.At); n != 0 {
		return n
	}
	return cmp.Compare(markerKey(a), markerKey(b))
}
