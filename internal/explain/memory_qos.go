package explain

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

type BoundaryState string

const (
	BoundaryUnavailable BoundaryState = "unavailable"
	BoundaryZero        BoundaryState = "zero"
	BoundaryFinite      BoundaryState = "finite"
	BoundaryUnlimited   BoundaryState = "unlimited"
)

type QoSBoundary struct {
	State BoundaryState `json:"state"`
	Bytes uint64        `json:"bytes"`
}

type QoSState string

const (
	QoSObserved     QoSState = "observed"
	QoSUnavailable  QoSState = "unavailable"
	QoSStale        QoSState = "stale"
	QoSResizing     QoSState = "resize-unsettled"
	QoSInconsistent QoSState = "potentially-stale-or-inconsistent"
)

type ThrottleActivity string

const (
	ThrottleUnreported        ThrottleActivity = "unreported"
	ThrottleNotCrossed        ThrottleActivity = "no-recent-crossing"
	ThrottleCrossed           ThrottleActivity = "crossed"
	ThrottleCrossedWithStalls ThrottleActivity = "crossed-with-stalls"
	ThrottleStallsOnly        ThrottleActivity = "stalls-without-recent-crossing"
)

type MemoryQoS struct {
	Scope              string              `json:"scope"`
	State              QoSState            `json:"state"`
	Confidence         Confidence          `json:"confidence"`
	Min                QoSBoundary         `json:"reclaimProtectionMin"`
	Low                QoSBoundary         `json:"reclaimProtectionLow"`
	High               QoSBoundary         `json:"throttleHigh"`
	Max                QoSBoundary         `json:"hardLimitMax"`
	Activity           ThrottleActivity    `json:"activity"`
	EventSource        string              `json:"eventSource"`
	HighDelta          uint64              `json:"highDelta"`
	DeltaKnown         bool                `json:"deltaKnown"`
	DeltaStart         time.Time           `json:"deltaStart,omitzero"`
	ObservedAt         time.Time           `json:"observedAt,omitzero"`
	PSIKnown           bool                `json:"psiKnown"`
	PSISomeAvg10       float64             `json:"psiSomeAvg10"`
	PSIFullAvg10       float64             `json:"psiFullAvg10"`
	RequestReference   model.ResourceValue `json:"requestReference"`
	RequestSource      string              `json:"requestSource"`
	LimitReference     model.ResourceValue `json:"limitReference"`
	LimitSource        string              `json:"limitSource"`
	PodConfiguredLimit model.ResourceValue `json:"podConfiguredLimit"`
	Correlations       []string            `json:"correlations"`
	Caveats            []string            `json:"caveats"`
	SuggestedChecks    []string            `json:"suggestedChecks"`
}

// InterpretMemoryQoS interprets observed leaf controls, never kubelet feature
// configuration. Pod resources remain a separate reference, not parent samples.
func InterpretMemoryQoS(container api.ContainerSnapshot) MemoryQoS {
	m := container.Memory
	result := MemoryQoS{
		Scope: "container-cgroup", State: QoSObserved, Confidence: ConfidenceMedium,
		Min:        qosBoundary(m.MinBytes, m.MinKnown, m.MinUnlimited),
		Low:        qosBoundary(m.LowBytes, m.LowKnown, m.LowUnlimited),
		High:       qosBoundary(m.HighBytes, m.HighKnown, m.HighUnlimited),
		Max:        qosBoundary(m.MaxBytes, m.MaxKnown, m.MaxUnlimited),
		ObservedAt: container.CapturedAt, PSIKnown: m.PressureKnown,
		PSISomeAvg10: m.PSISomeAvg10, PSIFullAvg10: m.PSIFullAvg10,
		PodConfiguredLimit: container.Context.Resources.Pod.Configured.Limit,
		Caveats:            []string{"Observed cgroup controls do not prove that the kubelet MemoryQoS policy is enabled.", "Parent cgroup protection and throttling are not observed by leaf-container samples."},
	}
	result.RequestReference, result.RequestSource = qosReference(container.Context.Resources.Applied.Request, container.Context.MemoryRequestKnown, container.Context.MemoryRequestBytes)
	result.LimitReference, result.LimitSource = qosReference(container.Context.Resources.Applied.Limit, container.Context.MemoryLimitKnown, container.Context.MemoryLimitBytes)
	result.Activity, result.EventSource, result.HighDelta, result.DeltaKnown = throttleActivity(container)
	if result.DeltaKnown {
		result.DeltaStart = container.DeltaStartedAt
	}
	if result.Activity == ThrottleCrossedWithStalls {
		result.Confidence = ConfidenceHigh
	}
	correlateQoS(&result, container)
	switch {
	case container.Freshness == api.EvidenceFreshnessStale:
		result.State, result.Confidence = QoSStale, ConfidenceLow
		result.SuggestedChecks = appendUnique(result.SuggestedChecks, "Collect a fresh, complete authorised container snapshot before assessing MemoryQoS.")
		result.Caveats = append(result.Caveats, "The snapshot is stale; these controls and event deltas describe an earlier observation.")
	case !m.MinKnown || !m.LowKnown || !m.HighKnown || !m.MaxKnown || container.CapturedAt.IsZero():
		result.State, result.Confidence = QoSUnavailable, ConfidenceLow
		result.SuggestedChecks = appendUnique(result.SuggestedChecks, "Collect a fresh, complete authorised container snapshot before assessing MemoryQoS.")
		result.Caveats = append(result.Caveats, "Incomplete boundary evidence cannot establish the current protection or throttling configuration.")
	case resizeUnsettled(container.Context.Resources.Pod):
		result.State, result.Confidence = QoSResizing, ConfidenceLow
		result.SuggestedChecks = appendUnique(result.SuggestedChecks, "Wait for the reported resize to settle, then compare applied resources with cgroup controls.")
		result.Caveats = append(result.Caveats, "Resize is unsettled; configured, applied and cgroup values may describe different generations.")
	}
	if result.High.State == BoundaryFinite || result.High.State == BoundaryZero {
		result.Caveats = append(result.Caveats, "Finite memory.high requires corrected kernel reclaim behaviour (Linux 5.9+) and a compatible CRI runtime; their versions are not collected here.")
	}
	return result
}

func qosBoundary(bytes uint64, known, unlimited bool) QoSBoundary {
	switch {
	case !known:
		return QoSBoundary{State: BoundaryUnavailable}
	case unlimited:
		return QoSBoundary{State: BoundaryUnlimited}
	case bytes == 0:
		return QoSBoundary{State: BoundaryZero}
	default:
		return QoSBoundary{State: BoundaryFinite, Bytes: bytes}
	}
}

func qosReference(applied model.ResourceValue, configured bool, bytes uint64) (model.ResourceValue, string) {
	if applied.Known {
		return applied, "kubelet-applied"
	}
	if configured {
		return model.ResourceValue{Known: true, Bytes: bytes}, "container-spec"
	}
	return model.ResourceValue{}, "unreported"
}

func resizeUnsettled(resources model.PodMemoryResources) bool {
	return resources.Pending.State != model.ResizeNone || resources.Applying.State != model.ResizeNone ||
		(resources.ObservedGeneration > 0 && resources.Generation > resources.ObservedGeneration)
}

func throttleActivity(container api.ContainerSnapshot) (ThrottleActivity, string, uint64, bool) {
	m := container.Memory
	delta, known := m.HighEventDelta()
	source := "memory.events"
	if m.LocalEventsKnown {
		source = "memory.events.local"
	}
	if !known || !container.DeltaWindowKnown || container.DeltaStartedAt.IsZero() || !container.DeltaStartedAt.Before(container.CapturedAt) {
		return ThrottleUnreported, source, 0, false
	}
	stalls := m.PressureKnown && (m.PSISomeAvg10 > 0 || m.PSIFullAvg10 > 0)
	switch {
	case delta > 0 && stalls:
		return ThrottleCrossedWithStalls, source, delta, true
	case delta > 0:
		return ThrottleCrossed, source, delta, true
	case stalls:
		return ThrottleStallsOnly, source, 0, true
	default:
		return ThrottleNotCrossed, source, 0, true
	}
}
