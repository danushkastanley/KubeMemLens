package nodecontext

import (
	"encoding/json"
	"math"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/resourcemetrics"
	"k8s.io/apimachinery/pkg/util/validation"
)

// Validate checks the private ingestion contract independently of the source
// adapter. Timestamps remain source times; admission never rewrites samples.
func Validate(o Observation, now time.Time, maxAge, futureSkew time.Duration) error {
	body, err := json.Marshal(o)
	if err != nil || len(body) > MaxObservationBytes || len(validation.IsDNS1123Subdomain(o.NodeName)) != 0 ||
		o.NodeUID == "" || len(o.NodeUID) > MaxNodeUIDBytes || strings.ContainsAny(o.NodeUID, "\x00\r\n\t ") {
		return ErrInvalidObservation
	}
	validTime := func(at time.Time) bool {
		return !at.IsZero() && !at.Before(now.Add(-maxAge)) && !at.After(now.Add(futureSkew)) && !at.After(o.ReportedAt.Add(futureSkew))
	}
	e := o.Evidence
	if !validTime(o.ReportedAt) || !validTime(e.ReceivedAt) || e.Source != Source || e.Scope != capability.NodeScope ||
		e.APIVersion != "v1alpha1" || e.Stability != capability.ImplementationSpecific || e.Window != 0 || len(e.Caveats) > MaxCaveats {
		return ErrInvalidObservation
	}
	for _, caveat := range e.Caveats {
		switch caveat {
		case "stats-provenance-unknown":
		case "optional-fields-unreported":
			if e.Completeness == capability.Complete {
				return ErrInvalidObservation
			}
		default:
			return ErrInvalidObservation
		}
	}
	if e.Completeness != capability.Complete && e.Completeness != capability.Partial {
		return ErrInvalidObservation
	}
	if err := validateContext(o.Context, validTime); err != nil {
		return err
	}
	if o.Availability != capability.Available {
		if e.Completeness != capability.Partial || o.Stats != nil || !e.CapturedAt.IsZero() || e.Freshness != capability.UnknownFreshness || !validFailure(o.Availability, o.Reason) {
			return ErrInvalidObservation
		}
		return nil
	}
	if o.Stats == nil || o.Reason != "" || !validTime(e.CapturedAt) || (e.Freshness != capability.Fresh && e.Freshness != capability.Stale) {
		return ErrInvalidObservation
	}
	stats := o.Stats
	if e.Completeness == capability.Complete && (!StatsComplete(*stats) || o.Context == nil || o.Context.CapacityBytes == nil || o.Context.AllocatableBytes == nil) {
		return ErrInvalidObservation
	}
	if !HasMeasurements(*stats) {
		return ErrInvalidObservation
	}
	if stats.StartedAt.IsZero() || stats.StartedAt.After(e.CapturedAt) || len(stats.SystemContainers) > MaxSystemContainers {
		return ErrInvalidObservation
	}
	switch stats.Provenance {
	case Unknown, CAdvisor, CRI:
	default:
		return ErrInvalidObservation
	}
	earliest := time.Time{}
	check := func(memory *Memory, swap *Swap, start time.Time) error {
		times := []time.Time{}
		if memory != nil {
			times = append(times, memory.CapturedAt)
			if err := validatePSI(memory.PSI); err != nil {
				return err
			}
		}
		if swap != nil {
			times = append(times, swap.CapturedAt)
		}
		for _, at := range times {
			if !validTime(at) || at.Before(stats.StartedAt) || at.Before(start) {
				return ErrInvalidObservation
			}
			if earliest.IsZero() || at.Before(earliest) {
				earliest = at
			}
		}
		return nil
	}
	if err := check(stats.Memory, stats.Swap, stats.StartedAt); err != nil {
		return err
	}
	seen := map[SystemCategory]bool{}
	for _, item := range stats.SystemContainers {
		switch item.Category {
		case Kubelet, Runtime, Misc, Pods:
		default:
			return ErrInvalidObservation
		}
		if seen[item.Category] || item.StartedAt.After(o.ReportedAt.Add(futureSkew)) {
			return ErrInvalidObservation
		}
		seen[item.Category] = true
		if err := check(item.Memory, item.Swap, item.StartedAt); err != nil {
			return err
		}
	}
	if earliest.IsZero() || !earliest.Equal(e.CapturedAt) {
		return ErrInvalidObservation
	}
	return nil
}

func validFailure(availability capability.Availability, reason Reason) bool {
	switch availability {
	case capability.Forbidden:
		return reason == Forbidden
	case capability.Unsupported:
		return reason == Unsupported
	case capability.Unreported:
		return reason == NotObserved
	case capability.Unavailable:
		switch reason {
		case InvalidTarget, UntrustedTLS, Authentication, TimedOut, Unreachable, InvalidResponse, ResponseTooLarge, Throttled, SourceUnavailable:
			return true
		}
	}
	return false
}

func validatePSI(psi *PSI) error {
	if psi == nil {
		return nil
	}
	for _, row := range []PSIData{psi.Some, psi.Full} {
		for _, value := range []float64{row.Avg10, row.Avg60, row.Avg300} {
			if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 100 {
				return ErrInvalidObservation
			}
		}
	}
	return nil
}

func validateContext(value *KubernetesContext, validTime func(time.Time) bool) error {
	if value == nil {
		return nil
	}
	if !validTime(value.CapturedAt) || len(value.Hugepages) > MaxHugepages {
		return ErrInvalidObservation
	}
	switch value.MemoryPressure {
	case "True", "False", "Unknown":
	default:
		return ErrInvalidObservation
	}
	seen := map[string]bool{}
	for _, page := range value.Hugepages {
		if !strings.HasPrefix(page.Resource, "hugepages-") || len(page.Resource) > MaxResourceBytes ||
			len(validation.IsQualifiedName(page.Resource)) != 0 || seen[page.Resource] {
			return ErrInvalidObservation
		}
		size, valid := resourcemetrics.QuantityBytes(strings.TrimPrefix(page.Resource, "hugepages-"))
		if !valid || size == 0 {
			return ErrInvalidObservation
		}
		seen[page.Resource] = true
	}
	return nil
}
