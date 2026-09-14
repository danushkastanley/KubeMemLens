package traceframe

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

// SafeText rejects malformed UTF-8 and renders terminal/format controls visibly.
// JSON escaping alone is insufficient: a JSON reader would restore the controls.
func SafeText(value string, maxBytes uint64) (string, error) {
	if maxBytes == 0 || maxBytes > 512 || uint64(len(value)) > maxBytes || !utf8.ValidString(value) {
		return "", ErrInvalid
	}
	var out strings.Builder
	for _, r := range value {
		if r == '\\' {
			out.WriteString("\\\\")
			continue
		}
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			fmt.Fprintf(&out, "\\u{%04x}", r)
			continue
		}
		out.WriteRune(r)
	}
	return out.String(), nil
}

func NewFile(event trace.FileActivity, spec trace.Specification) (Frame, error) {
	if spec.Validate() != nil || spec.Kind() != trace.Files || event.ObservedAt.IsZero() {
		return Frame{}, ErrInvalid
	}
	switch event.Operation {
	case trace.FileRead, trace.FileWrite, trace.FileOpen:
	default:
		return Frame{}, ErrInvalid
	}
	file := wireFile{Operation: event.Operation, RequestedBytes: event.RequestedBytes, CompletedBytes: event.CompletedBytes}
	if spec.Paths() == trace.ConfirmedPaths {
		path, err := SafeText(event.Path.Reveal(), spec.Bounds().PathBytes)
		if err != nil {
			return Frame{}, err
		}
		file.Path = &path
	}
	return makeFrame(envelope{Type: EventFrame, Event: &wireEvent{ObservedAt: event.ObservedAt.UTC(), File: &file}})
}
func NewCache(event trace.CacheActivity, spec trace.Specification) (Frame, error) {
	if spec.Validate() != nil || spec.Kind() != trace.Cache || event.ObservedAt.IsZero() || event.Pages == 0 {
		return Frame{}, ErrInvalid
	}
	switch event.Operation {
	case trace.CacheAdd, trace.CacheRemove:
	default:
		return Frame{}, ErrInvalid
	}
	return makeFrame(envelope{Type: EventFrame, Event: &wireEvent{ObservedAt: event.ObservedAt.UTC(), Cache: &wireCache{event.Operation, event.Pages}}})
}
func NewOOM(event trace.OOMDecision, spec trace.Specification) (Frame, error) {
	if spec.Validate() != nil || spec.Kind() != trace.OOM || event.ObservedAt.IsZero() {
		return Frame{}, ErrInvalid
	}
	switch event.Scope {
	case trace.OOMScopeUnknown, trace.OOMScopeCgroup, trace.OOMScopeGlobal:
	default:
		return Frame{}, ErrInvalid
	}
	if event.VictimPID != nil && *event.VictimPID == 0 {
		return Frame{}, ErrInvalid
	}
	command, err := SafeText(event.Command.Reveal(), 16)
	if err != nil {
		return Frame{}, err
	}
	return makeFrame(envelope{Type: EventFrame, Event: &wireEvent{ObservedAt: event.ObservedAt.UTC(), OOM: &wireOOM{event.Scope, event.VictimPID, command}}})
}
