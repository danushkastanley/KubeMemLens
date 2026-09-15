package traceframe

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceevidence"
)

const fieldNames = "version|type|metadata|event|summary|sessionID|engineDigest|programmeDigest|kind|target|paths|bounds|sessionStartedAt|deadline|namespace|pod|podUID|container|containerStartedAt|bindingDigest|durationNanos|events|outputBytes|mapBytes|pathBytes|observedAt|file|cache|oom|operation|requestedBytes|completedBytes|path|pages|scope|victimPID|command|sessionEndedAt|observationStartedAt|observationEndedAt|termination|engineCounts|produced|sampled|lost|rejected|writtenEvents|rejectedEvents|writtenBytesBeforeSummary|incomplete|fileAggregates|cacheAggregates|observations|reads|writes|additions|removals|operations|totalRequested|totalCompleted|totalPages|value|unreported|overflow|correlation|state|evidenceStart|beforeEnd|afterStart|evidenceEnd|overlapStart|overlapEnd|uncertaintyNanos|fileBytes|dirtyBytes|writebackBytes|before|after|refault|scan|steal|delta|oomAggregates|cgroup|global|unknown|missingProcessContext|oomCorrelation|window|local|hierarchical|currentBytes|limitBefore|limitAfter|bytes|someStallMicros|fullStallMicros|low|high|max|oomEvents|oomKills|oomGroupKills|kubernetesContext|beforeStart|afterEnd|restarts|pressureBefore|pressureAfter"

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
	if decoder.Decode(&e) != nil || !SupportedVersion(e.Version) {
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
		if e.Metadata == nil || validateMetadata(*e.Metadata) != nil || !AllowsKind(e.Version, e.Metadata.Kind) {
			return Frame{}, ErrInvalid
		}
	case EventFrame:
		if e.Event == nil || validateEvent(*e.Event) != nil {
			return Frame{}, ErrInvalid
		}
		if e.Version == AggregateVersion && validateAggregateEvent(*e.Event) != nil {
			return Frame{}, ErrInvalid
		}
		if e.Version == OOMVersion && validateOOMVersionEvent(*e.Event) != nil {
			return Frame{}, ErrInvalid
		}
	case SummaryFrame:
		if e.Summary == nil || validateSummary(*e.Summary, e.Version) != nil {
			return Frame{}, ErrInvalid
		}
	default:
		return Frame{}, ErrInvalid
	}
	// Retain the received representation: forwarding must account for its actual
	// bytes, including legal whitespace, rather than a shorter re-encoding.
	return Frame{kind: e.Type, data: string(data), version: e.Version}, nil
}

func uniqueValue(decoder *json.Decoder, depth int, field string) error {
	if depth > 5 {
		return ErrInvalid
	}
	token, err := decoder.Token()
	if err != nil {
		return ErrInvalid
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		if token == nil && !strings.Contains("|requestedBytes|completedBytes|victimPID|produced|sampled|lost|rejected|observationStartedAt|observationEndedAt|value|before|after|delta|bytes|", "|"+field+"|") {
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
func validateSummary(s wireSummary, version int) error {
	if (s.FileAggregates != nil && s.CacheAggregates != nil) || validateOOMAggregates(s) != nil {
		return ErrInvalid
	}
	var start, end time.Time
	if s.ObservationStartedAt != nil && s.ObservationEndedAt != nil {
		start, end = *s.ObservationStartedAt, *s.ObservationEndedAt
	}
	correlation, err := traceevidence.Decode(s.Correlation, start, end, 5*time.Minute)
	if err != nil {
		return ErrInvalid
	}
	oom, err := traceevidence.OOMDecode(s.OOMCorrelation, start, end, 5*time.Minute)
	if err != nil {
		return ErrInvalid
	}
	kubernetes, err := traceevidence.KubernetesOOMDecode(s.KubernetesContext, 5*time.Minute)
	if err != nil {
		return ErrInvalid
	}
	_, err = NewSummaryVersion(Summary{SessionEndedAt: s.SessionEndedAt, ObservationStartedAt: s.ObservationStartedAt, ObservationEndedAt: s.ObservationEndedAt, Termination: s.Termination, EngineCounts: trace.Counts{Produced: s.EngineCounts.Produced, Sampled: s.EngineCounts.Sampled, Lost: s.EngineCounts.Lost, Rejected: s.EngineCounts.Rejected}, WrittenEvents: s.WrittenEvents, RejectedEvents: s.RejectedEvents, WrittenBytesBeforeSummary: s.WrittenBytesBeforeSummary, Incomplete: s.Incomplete, Aggregates: domainAggregates(s), Correlation: correlation, OOMCorrelation: oom, KubernetesContext: kubernetes}, version)
	return err
}
