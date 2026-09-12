package volumecontext

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

// ValidateView checks the untrusted named response without inventing private
// UID bindings. Resource authorisation remains the server's responsibility.
func ValidateView(view View, now time.Time) error {
	if view.SchemaVersion != SchemaVersion || !validName(view.Namespace) || !validName(view.PodName) || len(view.Volumes) > MaxVolumesPerPod {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, v := range view.Volumes {
		if !validEvidenceID(v.EvidenceID) || !validEvidenceID(v.FilesystemID) || (v.FilesystemID != "" && (v.EvidenceID == "" || v.PVCName == "")) {
			return ErrInvalid
		}
		if !validName(v.VolumeName) || len(v.VolumeName) > 63 || seen[v.VolumeName] || (v.PVCName != "" && !validName(v.PVCName)) || (v.Driver != "" && !validName(v.Driver)) {
			return ErrInvalid
		}
		seen[v.VolumeName] = true
		c := v.Configuration
		if c.MountCount < 0 || c.MountCount > MaxMountsPerVolume || c.ReadOnlyMountCount < 0 || c.ReadOnlyMountCount > c.MountCount {
			return ErrInvalid
		}
		switch c.Kind {
		case PersistentClaim, EphemeralClaim, InlineCSI, EmptyDir, Other:
		default:
			return ErrInvalid
		}
		if c.Kind != EmptyDir && (c.MemoryBacked || c.SizeLimitBytes != nil) {
			return ErrInvalid
		}
		if c.Kind != PersistentClaim && c.Kind != EphemeralClaim && v.PVCName != "" {
			return ErrInvalid
		}
		if err := validateViewUsage(v.Usage, now); err != nil {
			return err
		}
		if len(v.Health) > 3 {
			return ErrInvalid
		}
		sources := map[volumehealth.Source]bool{}
		for _, h := range v.Health {
			if sources[h.Observation.Source] {
				return ErrInvalid
			}
			sources[h.Observation.Source] = true
			if err := validateViewHealth(h, now); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateViewUsage(u Usage, now time.Time) error {
	if u.Source != "kubelet-summary" {
		return ErrInvalid
	}
	if u.Availability == volumehealth.Reported {
		if u.Reason != "" || u.Filesystem == nil || u.LastGood != nil || (u.Freshness != volumehealth.Fresh && u.Freshness != volumehealth.Stale) {
			return ErrInvalid
		}
		if err := validateFilesystem(*u.Filesystem, now); err != nil {
			return err
		}
		expected := evaluateUsage(*u.Filesystem, u.Filesystem.CapturedAt)
		if u.Completeness != expected.Completeness {
			return ErrInvalid
		}
		return nil
	}
	if u.Filesystem != nil || u.Completeness != capability.Partial {
		return ErrInvalid
	}
	switch u.Availability {
	case volumehealth.Unreported:
		if u.Reason != NoReport && u.Reason != Expired {
			return ErrInvalid
		}
	case volumehealth.Unavailable:
		if u.Reason != SourceFailed {
			return ErrInvalid
		}
	default:
		state := u
		state.LastGood = nil
		if u.LastGood != nil || validateUsageState(state) != nil {
			return ErrInvalid
		}
	}
	if u.LastGood != nil {
		if u.Freshness != volumehealth.Stale {
			return ErrInvalid
		}
		return validateFilesystem(*u.LastGood, now)
	}
	if u.Freshness != volumehealth.FreshnessUnknown {
		return ErrInvalid
	}
	return nil
}

func validateViewHealth(h Health, now time.Time) error {
	if err := validateHealthReport(h.HealthReport, now); err != nil {
		return err
	}
	if h.LastGood == nil {
		return nil
	}
	if (h.Observation.Availability != volumehealth.Unavailable && h.Observation.Availability != volumehealth.Unreported) || h.LastGood.Observation.Availability != volumehealth.Reported || h.LastGood.Observation.Source != h.Observation.Source || h.LastGood.Observation.Scope != h.Observation.Scope || h.LastGood.Observation.ObservedAt.After(h.Observation.ObservedAt) || h.LastGood.Observation.ObservationFreshness != volumehealth.Stale {
		return ErrInvalid
	}
	return validateHealthReport(*h.LastGood, now)
}

func validateHealthReport(h HealthReport, now time.Time) error {
	if len(h.Conditions) > volumehealth.MaxConditions {
		return ErrInvalid
	}
	input := h.Observation
	if input.ObservedAt.After(now.Add(FutureSkew)) || (input.Availability == volumehealth.Reported && input.ObservedAt.IsZero()) {
		return ErrInvalid
	}
	input.TransitionAt = h.TransitionAt
	for _, c := range h.Conditions {
		input.Conditions = append(input.Conditions, volumehealth.Condition{Status: c.Status, Reason: c.Reason, TransitionAt: c.TransitionAt, AccessMode: c.AccessMode, VolumeMode: c.VolumeMode})
	}
	if err := validateHealth(input); err != nil {
		return err
	}
	expected := volumehealth.Evaluate(input, input.ObservedAt)
	if input.ObservationFreshness == volumehealth.Stale && input.Availability == volumehealth.Reported {
		expected.State = volumehealth.StateStale
		expected.ObservationFreshness = volumehealth.Stale
	}
	if input.Adverse != expected.Adverse || input.UnknownStatus != expected.UnknownStatus || input.State != expected.State || input.ObservationFreshness != expected.ObservationFreshness || input.ProbeFreshness != expected.ProbeFreshness {
		return ErrInvalid
	}
	return nil
}
