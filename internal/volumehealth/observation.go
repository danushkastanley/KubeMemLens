package volumehealth

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const MaxConditions = 16

// Evaluate preserves adverse evidence even when its API observation has aged.
// Kubernetes transition times are not probe heartbeats: probe freshness stays
// unknown, including when an old transition is read successfully just now.
func Evaluate(input Observation, now time.Time) Observation {
	o := input
	o.Conditions = append([]Condition(nil), input.Conditions...)
	o.ProbeFreshness = FreshnessUnknown
	o.ObservationFreshness = FreshnessUnknown
	o.Adverse = len(o.Conditions) > 0
	o.UnknownStatus = false
	o.State = stateForAvailability(o.Availability)
	for i := range o.Conditions {
		c := &o.Conditions[i]
		o.UnknownStatus = o.UnknownStatus || !knownStatus(o.Source, c.Status)
		c.Reason, o.TextTruncated = boundedText(c.Reason, 256, o.TextTruncated)
		c.Message, o.TextTruncated = boundedText(c.Message, 1024, o.TextTruncated)
	}
	if o.Availability != Reported {
		return o
	}
	o.State = StateHealthy
	if o.Adverse {
		o.State = StateAdverse
	}
	if o.ObservedAt.IsZero() || o.ObservedAt.After(now.Add(5*time.Second)) {
		return o
	}
	o.ObservationFreshness = Fresh
	if now.Sub(o.ObservedAt) > 2*time.Minute {
		o.ObservationFreshness = Stale
		o.State = StateStale
	}
	return o
}

func stateForAvailability(a Availability) State {
	switch a {
	case Disabled, Unsupported, Unreported, Forbidden, Unavailable:
		return State(a)
	default:
		return StateUnknown
	}
}

func knownStatus(source Source, status Status) bool {
	switch source {
	case PodSource, ControllerSource:
		return status == "Inaccessible" || status == "DataLoss" || status == "Degraded"
	case BackendSource:
		return status == "StorageUnreachable" || status == "StorageDegraded"
	default:
		return false
	}
}

func boundedText(value string, limit int, truncated bool) (string, bool) {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	if len(value) <= limit {
		return value, truncated
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value, true
}
