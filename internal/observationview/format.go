package observationview

import (
	"fmt"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

const RestrictedHelp = "Working set is Kubernetes Metrics API memory. It is not cgroup charge or a composition breakdown."
const DeepUnavailable = "Composition, local OOM deltas, reclaim counters, PSI and trace are unavailable: deep evidence is required."

func MemoryLabel(mode capability.Mode) string {
	if mode == capability.Restricted {
		return "Working set"
	}
	return "Cgroup charge"
}

func Memory(row Row) string {
	if value := row.Bytes(); value != nil {
		return model.FormatCompactBytes(*value)
	}
	return "unreported"
}

func Evidence(row Row) capability.Envelope {
	if row.Cgroup != nil {
		return row.Cgroup.Evidence
	}
	if row.WorkingSet != nil {
		return row.WorkingSet.Evidence
	}
	return capability.Envelope{Freshness: capability.UnknownFreshness, Completeness: capability.Partial}
}

func Age(at, now time.Time) string {
	if at.IsZero() {
		return "unreported"
	}
	if at.After(now) {
		return "future"
	}
	return now.Sub(at).Round(time.Second).String()
}

func State(row Row, now time.Time) string {
	if row.Cgroup == nil && (row.WorkingSet == nil || row.WorkingSet.Bytes == nil) {
		if row.WorkingSet != nil && row.WorkingSet.Availability != "" {
			return string(row.WorkingSet.Availability)
		}
		return "unreported"
	}
	evidence := Evidence(row)
	if evidence.Freshness == capability.Stale || (!evidence.CapturedAt.IsZero() && now.Sub(evidence.CapturedAt) > 2*time.Minute) {
		return "stale"
	}
	if evidence.Freshness != capability.Fresh {
		return string(capability.UnknownFreshness)
	}
	if evidence.Completeness != capability.Complete {
		return "partial"
	}
	return "fresh"
}

func Summary(row Row, now time.Time) []string {
	evidence := Evidence(row)
	name := row.Name
	if row.Namespace != "" {
		name = row.Namespace + "/" + name
	}
	lines := []string{row.Kind + ": " + name, "", MemoryLabel(row.Mode) + ": " + Memory(row), "Evidence state: " + State(row, now),
		"Source: " + string(evidence.Source), "API version: " + textOrUnreported(evidence.APIVersion),
		"Sample age: " + Age(evidence.CapturedAt, now), "Received age: " + Age(evidence.ReceivedAt, now)}
	if !evidence.CapturedAt.IsZero() {
		lines = append(lines, "Sampled: "+evidence.CapturedAt.UTC().Format(time.RFC3339Nano))
	}
	if evidence.Window > 0 {
		lines = append(lines, "Sample window: "+evidence.Window.String())
	}
	if row.WorkingSet != nil {
		value := row.WorkingSet
		lines = append(lines, fmt.Sprintf("Coverage: %d/%d %s observations", value.Coverage.Reported, value.Coverage.Expected, value.Coverage.Unit))
		if value.Reason != "" {
			lines = append(lines, "Reason: "+string(value.Reason))
		}
		if !value.LatestSampleAt.IsZero() && !value.LatestSampleAt.Equal(evidence.CapturedAt) {
			lines = append(lines, "Latest sample: "+value.LatestSampleAt.UTC().Format(time.RFC3339Nano))
		}
	}
	lines = append(lines, evidence.Caveats...)
	if row.Mode == capability.Restricted {
		lines = append(lines, "", RestrictedHelp, DeepUnavailable, "History requires deep evidence; restricted captures retain the available current observations.")
	}
	return lines
}

func textOrUnreported(value string) string {
	if value == "" {
		return "unreported"
	}
	return value
}
