package volumecontext

import (
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

var ErrOutOfOrder = errors.New("volume sample predates retained evidence")

// Samples contains current acquisition state and separately retained samples.
// It must not be serialised as a response without current binding checks.
type Samples struct {
	State    Usage      `json:"-"`
	Current  []RawUsage `json:"-"`
	LastGood []RawUsage `json:"-"`
}

// Retain merges only unexpired filesystem samples. The returned batch is an
// internal retention envelope: each measurement keeps its original timestamp.
// Its report time is the latest acquisition attempt, not a measurement time.
func Retain(previous *Batch, current Batch, now time.Time) (Batch, error) {
	rows := map[string]RawUsage{}
	if previous != nil && previous.nodeName == current.nodeName && previous.nodeUID == current.nodeUID {
		for _, row := range previous.records {
			if now.Sub(row.Filesystem.CapturedAt) <= ExpireAfter {
				rows[usageKey(row)] = row
			}
		}
	}
	for _, row := range current.records {
		key := usageKey(row)
		if prior, found := rows[key]; found {
			if row.Filesystem.CapturedAt.Before(prior.Filesystem.CapturedAt) {
				return Batch{}, ErrOutOfOrder
			}
			if row.Filesystem.CapturedAt.Equal(prior.Filesystem.CapturedAt) {
				if !sameFilesystemValues(row.Filesystem, prior.Filesystem) || row.PVCName != prior.PVCName || row.PVCNamespace != prior.PVCNamespace {
					return Batch{}, ErrOutOfOrder
				}
			}
		}
		rows[key] = row
	}
	keys := make([]string, 0, len(rows))
	for key := range rows {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]RawUsage, 0, len(keys))
	for _, key := range keys {
		values = append(values, rows[key])
	}
	return NewBatch(current.nodeName, current.nodeUID, current.reportedAt, SourceState(volumehealth.Reported, ""), values, now)
}

func usageKey(row RawUsage) string {
	return row.Namespace + "\x00" + row.PodUID + "\x00" + row.VolumeName
}

// JoinSamples is the acquisition-to-response seam. It selects only currently
// authorised bindings and never retains last-good numbers after a denial or
// explicit disable/unsupported decision.
func JoinSamples(scope PodScope, bindings []Binding, samples Samples, health []HealthObservation, now time.Time) (Report, error) {
	if len(bindings) > MaxVolumesPerPod || len(samples.Current) > MaxVolumesPerPod || len(samples.LastGood) > MaxVolumesPerPod || len(health) > 3*MaxVolumesPerPod {
		return Report{}, ErrInvalid
	}
	current := eligibleUsage(scope, bindings, samples.Current)
	result, err := Join(scope, bindings, current, health, samples.State, now)
	if err != nil {
		return Report{}, err
	}
	prior, err := Join(scope, bindings, eligibleUsage(scope, bindings, samples.LastGood), nil, SourceState(volumehealth.Reported, ""), now)
	if err != nil {
		return Report{}, err
	}
	for i := range result.volumes {
		row := &result.volumes[i]
		if row.Usage.Filesystem != nil {
			continue
		}
		if row.Usage.Availability != volumehealth.Unreported && row.Usage.Availability != volumehealth.Unavailable {
			continue
		}
		row.Usage.LastGood = cloneFilesystem(prior.volumes[i].Usage.Filesystem)
		if row.Usage.LastGood != nil {
			row.Usage.Freshness = volumehealth.Stale
		}
	}
	encoded, err := json.Marshal(result.Authorised())
	if err != nil || len(encoded) > MaxPageBytes {
		return Report{}, ErrInvalid
	}
	return result, nil
}

func sameFilesystemValues(a, b Filesystem) bool {
	left := [6]*uint64{a.CapacityBytes, a.UsedBytes, a.AvailableBytes, a.Inodes, a.InodesUsed, a.InodesFree}
	right := [6]*uint64{b.CapacityBytes, b.UsedBytes, b.AvailableBytes, b.Inodes, b.InodesUsed, b.InodesFree}
	for i, value := range left {
		if (value == nil) != (right[i] == nil) || (value != nil && *value != *right[i]) {
			return false
		}
	}
	return true
}

func eligibleUsage(scope PodScope, bindings []Binding, rows []RawUsage) []RawUsage {
	byName := make(map[string]Binding, len(bindings))
	for _, b := range bindings {
		byName[b.VolumeName] = b
	}
	selected := []RawUsage{}
	for _, row := range rows {
		b, exists := byName[row.VolumeName]
		if !exists || row.Namespace != scope.Namespace || row.PodUID != scope.PodUID || row.NodeUID != scope.NodeUID {
			continue
		}
		if b.ClaimAvailability != "" && b.ClaimAvailability != volumehealth.Reported {
			continue
		}
		if row.PVCName != b.PVCName || row.Filesystem.CapturedAt.Before(scope.CreatedAt) || (!b.PVCCreatedAt.IsZero() && row.Filesystem.CapturedAt.Before(b.PVCCreatedAt)) {
			continue
		}
		selected = append(selected, row)
	}
	return selected
}
