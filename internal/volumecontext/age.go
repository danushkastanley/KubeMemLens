package volumecontext

import (
	"slices"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

// AgeView updates presentation freshness without extending source times or
// reviving expired values. It never supplies authorisation for a cached view.
func AgeView(view View, now time.Time) (View, error) {
	if err := ValidateView(view, now); err != nil {
		return View{}, err
	}
	view.Volumes = slices.Clone(view.Volumes)
	for i := range view.Volumes {
		v := &view.Volumes[i]
		v.Configuration.SizeLimitBytes = cloneNumber(v.Configuration.SizeLimitBytes)
		if v.Usage.Filesystem != nil {
			wasStale := v.Usage.Freshness == volumehealth.Stale
			v.Usage = evaluateUsage(*v.Usage.Filesystem, now)
			if wasStale && v.Usage.Filesystem != nil {
				v.Usage.Freshness = volumehealth.Stale
			}
		}
		if v.Usage.LastGood != nil {
			v.Usage.LastGood = cloneFilesystem(v.Usage.LastGood)
			if now.Sub(v.Usage.LastGood.CapturedAt) > ExpireAfter {
				v.Usage.LastGood = nil
				v.Usage.Freshness = volumehealth.FreshnessUnknown
			}
		}
		v.Health = slices.Clone(v.Health)
		for j := range v.Health {
			h := &v.Health[j]
			h.HealthReport = ageHealthReport(h.HealthReport, now)
			if h.LastGood != nil {
				last := ageHealthReport(*h.LastGood, now)
				h.LastGood = &last
				if now.Sub(last.Observation.ObservedAt) > ExpireAfter {
					h.LastGood = nil
				}
			}
		}
	}
	return view, nil
}

func ageHealthReport(h HealthReport, now time.Time) HealthReport {
	h.Conditions = slices.Clone(h.Conditions)
	if h.Observation.Availability == volumehealth.Reported && now.Sub(h.Observation.ObservedAt) > ExpireAfter {
		h.Observation.State, h.Observation.ObservationFreshness = volumehealth.StateStale, volumehealth.Stale
	}
	return h
}
