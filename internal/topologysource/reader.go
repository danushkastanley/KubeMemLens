// Package topologysource reads fixed Linux memory attributes through rooted,
// read-only filesystem handles. It never enumerates workload cgroups or PIDs.
package topologysource

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/memorytopology"
)

const (
	MaxFileBytes        = 8192
	MaxReadBytes        = 1 << 20
	MaxFiles            = 384
	MaxDirectoryEntries = 32
	ReadTimeout         = 2 * time.Second
)

var errChanged = errors.New("topology membership changed")
var ErrBusy = errors.New("topology collection is already in progress")

type Paths struct{ System, Memory, Cgroup string }
type Reader struct {
	paths Paths
	now   func() time.Time
	mu    sync.Mutex
}
type readBudget struct {
	ctx          context.Context
	files, bytes int
}

func New(paths Paths, now func() time.Time) (*Reader, error) {
	for _, path := range []string{paths.System, paths.Memory, paths.Cgroup} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return nil, memorytopology.ErrInvalid
		}
	}
	if now == nil {
		now = time.Now
	}
	return &Reader{paths: paths, now: now}, nil
}

func (r *Reader) Read(ctx context.Context, name, uid string) (memorytopology.Observation, error) {
	now := r.now().UTC()
	result := memorytopology.Observation{SchemaVersion: 1, NodeName: name, NodeUID: uid, ReportedAt: now,
		NUMA: failed[memorytopology.NUMANode](memorytopology.NUMASysfs, fs.ErrNotExist), Pools: failed[memorytopology.HugePool](memorytopology.HugeTLBSysfs, fs.ErrNotExist), Cgroup: failed[memorytopology.HugeCgroup](memorytopology.HugeTLBCgroup, fs.ErrNotExist)}
	if err := result.Validate(now, memorytopology.FreshFor); err != nil {
		return result, err
	}
	if !r.mu.TryLock() {
		return result, ErrBusy
	}
	defer r.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, ReadTimeout)
	defer cancel()
	budget := &readBudget{ctx: ctx}
	root, err := os.OpenRoot(r.paths.Memory)
	if err != nil {
		result.Pools = failed[memorytopology.HugePool](memorytopology.HugeTLBSysfs, err)
	} else {
		pools, readErr := budget.pools(root, "hugepages", false)
		_ = root.Close()
		result.Pools = section(memorytopology.HugeTLBSysfs, pools, memorytopology.PoolsComplete(pools), r.now().UTC(), readErr)
	}
	root, err = os.OpenRoot(r.paths.System)
	if err != nil {
		result.NUMA = failed[memorytopology.NUMANode](memorytopology.NUMASysfs, err)
	} else {
		nodes, readErr := budget.nodes(root)
		_ = root.Close()
		result.NUMA = section(memorytopology.NUMASysfs, nodes, memorytopology.NUMAComplete(nodes), r.now().UTC(), readErr)
	}
	sizes := pageSizes(result)
	if len(sizes) > memorytopology.MaxPageSizes {
		result.Cgroup = failed[memorytopology.HugeCgroup](memorytopology.HugeTLBCgroup, memorytopology.ErrBounds)
	} else {
		result.Cgroup = r.cgroups(budget, sizes)
	}
	result.ReportedAt = r.now().UTC()
	if err := result.Validate(result.ReportedAt, memorytopology.FreshFor); err != nil {
		return memorytopology.Observation{}, err
	}
	return result, nil
}

func section[T any](source memorytopology.Source, items []T, complete bool, at time.Time, err error) memorytopology.Section[T] {
	if err != nil {
		return failed[T](source, err)
	}
	if len(items) == 0 {
		return memorytopology.Section[T]{Source: source, Availability: capability.Unreported, Completeness: capability.Partial, Reason: memorytopology.NotObserved}
	}
	s := memorytopology.Section[T]{Source: source, Availability: capability.Available, Completeness: capability.Partial, Reason: memorytopology.PartialFields, CapturedAt: at, Items: items}
	if complete {
		s.Completeness = capability.Complete
		s.Reason = ""
	}
	return s
}
func failed[T any](source memorytopology.Source, err error) memorytopology.Section[T] {
	state, reason := capability.Unavailable, memorytopology.SourceFailed
	switch {
	case errors.Is(err, fs.ErrNotExist):
		state, reason = capability.Unsupported, memorytopology.SourceAbsent
	case errors.Is(err, fs.ErrPermission):
		state, reason = capability.Forbidden, memorytopology.AccessDenied
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled):
		reason = memorytopology.TimedOut
	case errors.Is(err, memorytopology.ErrBounds):
		reason = memorytopology.SourceBounds
	case errors.Is(err, memorytopology.ErrInvalid):
		reason = memorytopology.InvalidSource
	case errors.Is(err, errChanged):
		reason = memorytopology.SourceChanged
	}
	return memorytopology.Section[T]{Source: source, Availability: state, Completeness: capability.Partial, Reason: reason}
}
