package traceframe

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

const fieldNames = "version|type|metadata|event|summary|sessionID|engineDigest|programmeDigest|kind|target|paths|bounds|sessionStartedAt|deadline|namespace|pod|podUID|container|containerStartedAt|bindingDigest|durationNanos|events|outputBytes|mapBytes|pathBytes|observedAt|file|cache|oom|operation|requestedBytes|completedBytes|path|pages|scope|victimPID|command|sessionEndedAt|observationStartedAt|observationEndedAt|termination|engineCounts|produced|sampled|lost|rejected|writtenEvents|rejectedEvents|writtenBytesBeforeSummary|incomplete"

// Decode accepts exactly one bounded NDJSON frame. It rejects unknown versions,
// fields, case aliases, duplicate keys, arrays and ambiguous unions. A Reader
// additionally enforces stream order and the metadata's cumulative limits.
func Decode(data []byte) (Frame, error) {
	if len(data) == 0 || len(data) > MaxBytes || data[len(data)-1] != '\n' || bytes.ContainsRune(data[:len(data)-1], '\n') || !utf8.Valid(data) {
		return Frame{}, ErrInvalid
	}
	keys := json.NewDecoder(bytes.NewReader(data))
	if uniqueValue(keys, 0, "root") != nil {
		return Frame{}, ErrInvalid
	}
	if _, err := keys.Token(); err != io.EOF {
		return Frame{}, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var e envelope
	if decoder.Decode(&e) != nil || e.Version != Version {
		return Frame{}, ErrInvalid
	}
	payloads := 0
	for _, present := range []bool{e.Metadata != nil, e.Event != nil, e.Summary != nil} {
		if present {
			payloads++
		}
	}
	if payloads != 1 {
		return Frame{}, ErrInvalid
	}
	switch e.Type {
	case MetadataFrame:
		if e.Metadata == nil || validateMetadata(*e.Metadata) != nil {
			return Frame{}, ErrInvalid
		}
	case EventFrame:
		if e.Event == nil || validateEvent(*e.Event) != nil {
			return Frame{}, ErrInvalid
		}
	case SummaryFrame:
		if e.Summary == nil || validateSummary(*e.Summary) != nil {
			return Frame{}, ErrInvalid
		}
	default:
		return Frame{}, ErrInvalid
	}
	// Retain the received representation: forwarding must account for its actual
	// bytes, including legal whitespace, rather than a shorter re-encoding.
	return Frame{kind: e.Type, data: string(data)}, nil
}

func uniqueValue(decoder *json.Decoder, depth int, field string) error {
	if depth > 3 {
		return ErrInvalid
	}
	token, err := decoder.Token()
	if err != nil {
		return ErrInvalid
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		if token == nil && !strings.Contains("|requestedBytes|completedBytes|victimPID|produced|sampled|lost|rejected|observationStartedAt|observationEndedAt|", "|"+field+"|") {
			return ErrInvalid
		}
		return nil
	}
	if delimiter != '{' {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] || !strings.Contains("|"+fieldNames+"|", "|"+key+"|") {
			return ErrInvalid
		}
		seen[key] = true
		if uniqueValue(decoder, depth+1, key) != nil {
			return ErrInvalid
		}
	}
	for _, required := range strings.Split(requiredFields(field), "|") {
		if !seen[required] {
			return ErrInvalid
		}
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') {
		return ErrInvalid
	}
	return nil
}

func validateMetadata(m wireMetadata) error {
	if !validID(m.SessionID) || !validDigest(m.EngineDigest) || !validDigest(m.ProgrammeDigest) || !validDigest(m.Target.BindingDigest) || m.bounds().Validate() != nil || m.SessionStartedAt.IsZero() || (!m.Deadline.After(m.SessionStartedAt) || m.Deadline.After(m.SessionStartedAt.Add(m.bounds().Duration))) || m.Target.ContainerStartedAt.IsZero() {
		return ErrInvalid
	}
	for _, value := range []string{m.Target.Namespace, m.Target.Pod, m.Target.PodUID, m.Target.Container} {
		safe, err := SafeText(value, 253)
		if err != nil || value == "" || safe != value {
			return ErrInvalid
		}
	}
	switch m.Kind {
	case trace.Files, trace.Cache, trace.OOM:
	default:
		return ErrInvalid
	}
	if m.Paths != trace.OmitPaths && m.Paths != trace.ConfirmedPaths {
		return ErrInvalid
	}
	if m.Paths == trace.ConfirmedPaths && m.Kind != trace.Files {
		return ErrInvalid
	}
	return nil
}
func validateEvent(e wireEvent) error {
	if e.ObservedAt.IsZero() {
		return ErrInvalid
	}
	count := 0
	for _, present := range []bool{e.File != nil, e.Cache != nil, e.OOM != nil} {
		if present {
			count++
		}
	}
	if count != 1 {
		return ErrInvalid
	}
	if e.File != nil {
		switch e.File.Operation {
		case trace.FileOpen, trace.FileRead, trace.FileWrite:
		default:
			return ErrInvalid
		}
		if e.File.Path != nil && !safeEncoded(*e.File.Path) {
			return ErrInvalid
		}
	}
	if e.Cache != nil {
		switch e.Cache.Operation {
		case trace.CacheAdd, trace.CacheRemove:
		default:
			return ErrInvalid
		}
		if e.Cache.Pages == 0 {
			return ErrInvalid
		}
	}
	if e.OOM != nil {
		switch e.OOM.Scope {
		case trace.OOMScopeUnknown, trace.OOMScopeCgroup, trace.OOMScopeGlobal:
		default:
			return ErrInvalid
		}
		_, textErr := DecodeText(e.OOM.Command, 16)
		if textErr != nil || (e.OOM.VictimPID != nil && *e.OOM.VictimPID == 0) {
			return ErrInvalid
		}
	}
	return nil
}
func safeEncoded(value string) bool { _, err := DecodeText(value, 512); return err == nil }
func validateSummary(s wireSummary) error {
	_, err := NewSummary(Summary{s.SessionEndedAt, s.ObservationStartedAt, s.ObservationEndedAt, s.Termination, trace.Counts{Produced: s.EngineCounts.Produced, Sampled: s.EngineCounts.Sampled, Lost: s.EngineCounts.Lost, Rejected: s.EngineCounts.Rejected}, s.WrittenEvents, s.RejectedEvents, s.WrittenBytesBeforeSummary, s.Incomplete})
	return err
}
