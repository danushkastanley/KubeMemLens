package volumecontext

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

// Join accepts only the caller's authorised Pod and bindings. Acquisition and
// authorisation belong to adapters; this boundary rejects cross-scope input.
// Usage state describes source availability independently of health reports.
func Join(scope PodScope, bindings []Binding, usage []RawUsage, health []HealthObservation, state Usage, now time.Time) (Report, error) {
	if err := validateScope(scope, now); err != nil {
		return Report{}, err
	}
	if len(bindings) > MaxVolumesPerPod || len(usage) > MaxVolumesPerPod || len(health) > 3*MaxVolumesPerPod {
		return Report{}, ErrInvalid
	}
	if err := validateUsageState(state); err != nil {
		return Report{}, err
	}
	rows := make(map[string]Volume, len(bindings))
	for _, binding := range bindings {
		if err := validateBinding(binding, now); err != nil {
			return Report{}, err
		}
		if _, exists := rows[binding.VolumeName]; exists {
			return Report{}, ErrInvalid
		}
		rows[binding.VolumeName] = Volume{Binding: cloneBinding(binding), Usage: state}
	}
	seenUsage := make(map[string]bool, len(usage))
	for _, raw := range usage {
		row, exists := rows[raw.VolumeName]
		if !exists || raw.Namespace != scope.Namespace || raw.PodUID != scope.PodUID || raw.NodeUID != scope.NodeUID {
			return Report{}, ErrScope
		}
		if seenUsage[raw.VolumeName] || state.Availability != volumehealth.Reported {
			return Report{}, ErrInvalid
		}
		seenUsage[raw.VolumeName] = true
		b := row.Binding
		if (b.Configuration.Kind == PersistentClaim || b.Configuration.Kind == EphemeralClaim) && b.ClaimAvailability != volumehealth.Reported {
			return Report{}, ErrScope
		}
		if raw.PVCName != b.PVCName || (b.PVCName != "" && raw.PVCNamespace != scope.Namespace) || (b.PVCName == "" && raw.PVCNamespace != "") {
			return Report{}, ErrScope
		}
		if err := validateFilesystem(raw.Filesystem, now); err != nil {
			return Report{}, err
		}
		at := raw.Filesystem.CapturedAt
		if at.Before(scope.CreatedAt) || (!b.PVCCreatedAt.IsZero() && at.Before(b.PVCCreatedAt)) {
			return Report{}, ErrScope
		}
		row.Usage = evaluateUsage(raw.Filesystem, now)
		rows[raw.VolumeName] = row
	}
	seenHealth := make(map[string]bool, len(health))
	for _, input := range health {
		id := input.Identity
		row, exists := rows[id.VolumeName]
		if !exists || id.Namespace != scope.Namespace || id.PodName != scope.PodName || id.PodUID != scope.PodUID || id.NodeName != scope.NodeName || input.NodeUID != scope.NodeUID {
			return Report{}, ErrScope
		}
		if (id.PVCName != "" && id.PVCName != row.Binding.PVCName) || (id.PVCUID != "" && id.PVCUID != row.Binding.PVCUID) ||
			(id.Driver != "" && id.Driver != row.Binding.Driver) {
			return Report{}, ErrScope
		}
		if input.Source == volumehealth.ControllerSource && input.Availability == volumehealth.Reported &&
			(id.PVCUID == "" || id.PVCName == "") {
			return Report{}, ErrScope
		}
		if input.Source == volumehealth.BackendSource && input.Availability == volumehealth.Reported && id.Driver == "" {
			return Report{}, ErrScope
		}
		if err := validateHealth(input.Observation); err != nil {
			return Report{}, err
		}
		key := id.VolumeName + "\x00" + string(input.Source)
		if seenHealth[key] {
			return Report{}, ErrInvalid
		}
		seenHealth[key] = true
		if input.ObservedAt.IsZero() || input.ObservedAt.After(now.Add(FutureSkew)) || input.ObservedAt.Before(scope.CreatedAt) {
			return Report{}, ErrInvalid
		}
		if input.Source == volumehealth.ControllerSource && !row.Binding.PVCCreatedAt.IsZero() && input.ObservedAt.Before(row.Binding.PVCCreatedAt) {
			return Report{}, ErrScope
		}
		value := volumehealth.Evaluate(input.Observation, now)
		// Messages can contain backend handles. They never enter retained context.
		for i := range value.Conditions {
			value.Conditions[i].Message = ""
		}
		row.Health = append(row.Health, value)
		rows[id.VolumeName] = row
	}
	result := Report{scope: scope, volumes: make([]Volume, 0, len(rows))}
	for _, row := range rows {
		if !seenUsage[row.Binding.VolumeName] && state.Availability == volumehealth.Reported {
			row.Usage = missingUsage()
		}
		if availability := row.Binding.ClaimAvailability; availability != "" && availability != volumehealth.Reported {
			reason := map[volumehealth.Availability]Reason{volumehealth.Forbidden: AccessDenied, volumehealth.Unavailable: SourceFailed, volumehealth.Unreported: NoReport}[availability]
			row.Usage = SourceState(availability, reason)
		}
		sort.Slice(row.Health, func(i, j int) bool { return row.Health[i].Source < row.Health[j].Source })
		result.volumes = append(result.volumes, row)
	}
	sort.Slice(result.volumes, func(i, j int) bool {
		return result.volumes[i].Binding.VolumeName < result.volumes[j].Binding.VolumeName
	})
	encoded, err := json.Marshal(result.Authorised())
	if err != nil || len(encoded) > MaxPageBytes {
		return Report{}, ErrInvalid
	}
	return result, nil
}

func missingUsage() Usage {
	return SourceState(volumehealth.Unreported, NoReport)
}

// SourceState is the non-numeric result of the source adapter. Unreported is
// the default when capability evidence cannot establish a more precise reason.
func SourceState(availability volumehealth.Availability, reason Reason) Usage {
	return Usage{Source: "kubelet-summary", Availability: availability, Reason: reason, Freshness: volumehealth.FreshnessUnknown, Completeness: capability.Partial}
}

func validateUsageState(u Usage) error {
	if u.Source != "kubelet-summary" || u.Filesystem != nil || u.Freshness != volumehealth.FreshnessUnknown || u.Completeness != capability.Partial {
		return ErrInvalid
	}
	want, valid := map[volumehealth.Availability]Reason{
		volumehealth.Reported: "", volumehealth.Unreported: NoReport, volumehealth.Disabled: Disabled,
		volumehealth.Unsupported: Unsupported, volumehealth.Forbidden: AccessDenied,
		volumehealth.Unavailable: SourceFailed, volumehealth.Unknown: BindingUnavailable,
	}[u.Availability]
	if !valid || want != u.Reason {
		return ErrInvalid
	}
	return nil
}

func evaluateUsage(f Filesystem, now time.Time) Usage {
	if now.Sub(f.CapturedAt) > ExpireAfter {
		u := missingUsage()
		u.Reason = Expired
		return u
	}
	u := SourceState(volumehealth.Reported, "")
	u.Filesystem = cloneFilesystem(&f)
	u.Freshness = volumehealth.Fresh
	if f.CapacityBytes != nil && f.UsedBytes != nil && f.AvailableBytes != nil && f.Inodes != nil && f.InodesUsed != nil && f.InodesFree != nil {
		u.Completeness = capability.Complete
	}
	if now.Sub(f.CapturedAt) > StaleAfter {
		u.Freshness = volumehealth.Stale
	}
	return u
}

func cloneBinding(b Binding) Binding {
	b.Configuration.SizeLimitBytes = cloneNumber(b.Configuration.SizeLimitBytes)
	return b
}

func cloneNumber(value *uint64) *uint64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneFilesystem(f *Filesystem) *Filesystem {
	if f == nil {
		return nil
	}
	return &Filesystem{CapturedAt: f.CapturedAt, CapacityBytes: cloneNumber(f.CapacityBytes), UsedBytes: cloneNumber(f.UsedBytes),
		AvailableBytes: cloneNumber(f.AvailableBytes), Inodes: cloneNumber(f.Inodes), InodesUsed: cloneNumber(f.InodesUsed), InodesFree: cloneNumber(f.InodesFree)}
}
