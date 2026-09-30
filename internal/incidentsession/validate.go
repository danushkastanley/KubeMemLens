package incidentsession

import (
	"encoding/hex"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"k8s.io/apimachinery/pkg/util/validation"
)

func (l Limits) valid() bool {
	return l.Sessions >= 1 && l.Sessions <= 64 && l.NamespaceSessions >= 1 && l.NamespaceSessions <= 8 &&
		l.NamespaceSessions <= l.Sessions && l.Entries >= 2 && l.Entries <= MaxEntries &&
		l.SessionBytes >= 16<<10 && l.SessionBytes <= MaxExportBytes && l.Retention >= time.Minute && l.Retention <= 4*time.Hour
}

func (p Principal) valid() bool {
	return len(validation.IsDNS1123Label(p.Namespace)) == 0 && text(p.NamespaceUID, 128) && text(p.Actor, 512)
}

func text(value string, limit int) bool {
	return value != "" && len(value) <= limit && utf8.ValidString(value) &&
		!strings.ContainsFunc(value, func(r rune) bool { return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) })
}

func digest(value string, size int) bool {
	if len(value) != size*2 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func (in Input) validate(p Principal) error {
	if len(in.References) > 2 {
		return ErrInvalid
	}
	if !validSource(in.Source) {
		return ErrInvalid
	}
	for _, ref := range in.References {
		if ref.NamespaceUID != p.NamespaceUID || !digest(ref.Digest, 32) || ref.SchemaVersion < 1 || ref.SchemaVersion > 7 || ref.ObservedAt.IsZero() {
			return ErrInvalid
		}
	}
	if in.Kind != Annotated && in.Note != "" {
		return ErrInvalid
	}
	if in.Kind != Gap && in.GapReason != "" {
		return ErrInvalid
	}
	if in.Kind != TraceReferenced && in.traceReference != nil {
		return ErrInvalid
	}
	switch in.Kind {
	case TraceReferenced:
		if in.Source != traceReferenceSource || len(in.References) != 0 || in.traceReference == nil ||
			in.traceReference.Validate() != nil || !in.ObservedAt.Equal(in.traceReference.CapturedAt) {
			return ErrInvalid
		}
	case Annotated:
		if in.Source != "operator" || !in.ObservedAt.IsZero() || len(in.References) != 0 || ValidateAnnotation(in.Note) != nil {
			return ErrInvalid
		}
	case Captured, Marked:
		if len(in.References) != 1 || in.ObservedAt.IsZero() {
			return ErrInvalid
		}
		if in.Kind == Marked && in.References[0].SchemaVersion != 6 {
			return ErrInvalid
		}
	case Compared:
		if len(in.References) != 2 || in.ObservedAt.IsZero() || !compatibleCaptureSchemas(in.References[0].SchemaVersion, in.References[1].SchemaVersion) {
			return ErrInvalid
		}
	case Gap:
		if len(in.References) != 0 {
			return ErrInvalid
		}
		if !validGap(in.GapReason) {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

func clonePrincipal(p Principal) Principal {
	return Principal{strings.Clone(p.Namespace), strings.Clone(p.NamespaceUID), strings.Clone(p.Actor)}
}

func newEntry(p Principal, in Input, now time.Time, sequence uint64) Entry {
	e := Entry{Sequence: sequence, Kind: Kind(strings.Clone(string(in.Kind))), RecordedAt: now.UTC(),
		Namespace: p.Namespace, NamespaceUID: p.NamespaceUID, Actor: p.Actor,
		Source: strings.Clone(in.Source), Sensitivity: "private",
		Note: strings.Clone(in.Note), GapReason: strings.Clone(in.GapReason)}
	observed := in.ObservedAt
	if in.Source == "operator" && observed.IsZero() {
		observed = now
	}
	if !observed.IsZero() {
		at := observed.UTC()
		e.ObservedAt = &at
	}
	for _, ref := range in.References {
		ref.NamespaceUID, ref.Digest = strings.Clone(ref.NamespaceUID), strings.Clone(ref.Digest)
		ref.ObservedAt = ref.ObservedAt.UTC()
		e.References = append(e.References, ref)
	}
	if in.traceReference != nil {
		ref := *in.traceReference
		e.TraceReference = &ref
	}
	return e
}

func validSource(source string) bool {
	switch source {
	case "operator", "collector", "kubernetes", "prometheus", traceReferenceSource:
		return true
	}
	return false
}

func validGap(reason string) bool {
	switch reason {
	case "source-disabled", "source-unsupported", "events-partial", "events-truncated", "events-missing", "events-denied", "events-disabled", "events-unavailable", "history-disabled", "history-stale", "history-missing", "history-unsupported", "history-unavailable", "source-capacity", "source-unavailable", "source-denied", "source-changed", "collector-restarted", "clock-uncertain", "partial-evidence":
		return true
	}
	return false
}

// ValidateAnnotation rejects text that JSON encoding would otherwise replace or
// that could alter terminal display. It is shared by client and server boundaries.
func ValidateAnnotation(note string) error {
	if !text(note, 512) {
		return ErrInvalid
	}
	return nil
}
