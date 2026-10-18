package memorytopology

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"k8s.io/apimachinery/pkg/util/validation"
)

// Validate uses maxAge=0 for historical reads; admission supplies a positive
// age ceiling. Future clocks and source identity are checked in both cases.
func (o Observation) Validate(now time.Time, maxAge time.Duration) error {
	if maxAge < 0 || o.SchemaVersion != SchemaVersion || len(validation.IsDNS1123Subdomain(o.NodeName)) != 0 || len(o.NodeUID) == 0 || len(o.NodeUID) > 128 || strings.IndexFunc(o.NodeUID, func(r rune) bool { return r < 33 || r > 126 }) >= 0 || !validTime(o.ReportedAt, now, maxAge) {
		return ErrInvalid
	}
	if err := validateSection(o.NUMA, NUMASysfs, MaxNodes, NUMAComplete(o.NUMA.Items), o.ReportedAt, now, maxAge); err != nil {
		return err
	}
	if err := validateSection(o.Pools, HugeTLBSysfs, MaxPageSizes, PoolsComplete(o.Pools.Items), o.ReportedAt, now, maxAge); err != nil {
		return err
	}
	if err := validateSection(o.Cgroup, HugeTLBCgroup, MaxPageSizes, CgroupsComplete(o.Cgroup.Items), o.ReportedAt, now, maxAge); err != nil {
		return err
	}
	ids := map[uint16]bool{}
	for _, n := range o.NUMA.Items {
		if ids[n.ID] {
			return ErrInvalid
		}
		ids[n.ID] = true
		if err := validatePools(n.Pools, true); err != nil {
			return err
		}
	}
	if err := validatePools(o.Pools.Items, false); err != nil {
		return err
	}
	sizes := map[uint64]bool{}
	for _, g := range o.Cgroup.Items {
		if !validPageSize(g.PageSizeBytes) || sizes[g.PageSizeBytes] || !validLimit(g.Limit) || !validLimit(g.ReservationLimit) {
			return ErrInvalid
		}
		sizes[g.PageSizeBytes] = true
		if len(g.NUMA) > MaxNodes {
			return ErrBounds
		}
		seen := map[uint16]bool{}
		for _, n := range g.NUMA {
			if seen[n.ID] || n.Bytes%g.PageSizeBytes != 0 {
				return ErrInvalid
			}
			seen[n.ID] = true
		}
		// Usage is charged in huge pages. Limits are reported byte values and
		// may expose a base-page counter ceiling rather than a huge-page multiple.
		for _, value := range []*uint64{g.CurrentBytes, g.ReservationBytes, g.NUMATotalBytes} {
			if value != nil && *value%g.PageSizeBytes != 0 {
				return ErrInvalid
			}
		}
	}
	body, err := json.Marshal(o)
	if err != nil {
		return ErrInvalid
	}
	if len(body) > MaxObservationBytes {
		return ErrBounds
	}
	return nil
}

func validTime(at, now time.Time, maxAge time.Duration) bool {
	return !at.IsZero() && (maxAge == 0 || !at.Before(now.Add(-maxAge))) && !at.After(now.Add(FutureSkew))
}
func validateSection[T any](s Section[T], source Source, limit int, complete bool, reported, now time.Time, maxAge time.Duration) error {
	if s.Source != source {
		return ErrInvalid
	}
	if len(s.Items) > limit {
		return ErrBounds
	}
	if s.Availability == capability.Available {
		if len(s.Items) == 0 || !validTime(s.CapturedAt, now, maxAge) || s.CapturedAt.After(reported.Add(FutureSkew)) {
			return ErrInvalid
		}
		switch s.Completeness {
		case capability.Complete:
			if !complete || s.Reason != "" {
				return ErrInvalid
			}
		case capability.Partial:
			if s.Reason != PartialFields && s.Reason != SourceChanged {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
		return nil
	}
	if len(s.Items) != 0 || !s.CapturedAt.IsZero() || s.Completeness != capability.Partial {
		return ErrInvalid
	}
	switch s.Availability {
	case capability.Disabled:
		if s.Reason != ProfileDisabled {
			return ErrInvalid
		}
	case capability.Forbidden:
		if s.Reason != AccessDenied {
			return ErrInvalid
		}
	case capability.Unsupported:
		if s.Reason != SourceAbsent {
			return ErrInvalid
		}
	case capability.Unreported:
		if s.Reason != NotObserved {
			return ErrInvalid
		}
	case capability.Unavailable:
		switch s.Reason {
		case InvalidSource, SourceBounds, SourceChanged, TimedOut, SourceFailed:
		default:
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}
func validatePools(pools []HugePool, perNode bool) error {
	if len(pools) > MaxPageSizes {
		return ErrBounds
	}
	seen := map[uint64]bool{}
	for _, p := range pools {
		if !validPageSize(p.PageSizeBytes) || seen[p.PageSizeBytes] || (perNode && p.ReservedPages != nil) {
			return ErrInvalid
		}
		seen[p.PageSizeBytes] = true
	}
	return nil
}
func validPageSize(n uint64) bool { return n >= 4096 && n <= 1<<50 && n&(n-1) == 0 }
func validLimit(l Limit) bool     { return !(l.Unlimited && l.Bytes != nil) }
