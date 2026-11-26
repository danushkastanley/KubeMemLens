package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/danushkastanley/kube-memlens/internal/cgroup"
	"github.com/danushkastanley/kube-memlens/internal/kube"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

// podCgroupReader reads each Pod cgroup at most once per scan and decides
// which containers may carry it. groups holds the scanner's unambiguous
// parent-directory groups: a directory whose entries all name one Pod in their
// cgroup path and map to one Pod.
type podCgroupReader struct {
	cgroupRoot string
	groups     map[string]struct{}
	index      kube.PodIndex
	root       *os.Root
	values     map[string]model.PodCgroupMemory
	errs       []error
}

func newPodCgroupReader(cgroupRoot string, groups map[string]struct{}, index kube.PodIndex) *podCgroupReader {
	return &podCgroupReader{cgroupRoot: cgroupRoot, groups: groups, index: index, values: map[string]model.PodCgroupMemory{}}
}

// forContainer returns the Pod cgroup for a container mapped to podUID. The
// container gets nothing unless its own directory is the Pod cgroup's direct
// child, the scanner grouped that Pod cgroup unambiguously, and the Pod UID in
// the cgroup path is the mapped Pod's.
func (r *podCgroupReader) forContainer(entry cgroup.Entry, podUID string) model.PodCgroupMemory {
	dir, found := cgroup.PodCgroupDir(entry.RelativePath)
	if !found || filepath.Dir(entry.RelativePath) != dir {
		return model.PodCgroupMemory{}
	}
	if _, unambiguous := r.groups[dir]; !unambiguous || !cgroupNamesPod(r.index, entry.PodUID, podUID) {
		return model.PodCgroupMemory{}
	}
	if value, read := r.values[dir]; read {
		return value
	}
	value, err := r.read(dir)
	if err != nil {
		r.errs = append(r.errs, fmt.Errorf("read Pod cgroup %s: %w", dir, err))
	}
	r.values[dir] = value
	return value
}

func (r *podCgroupReader) read(dir string) (model.PodCgroupMemory, error) {
	if r.root == nil {
		root, err := os.OpenRoot(r.cgroupRoot)
		if err != nil {
			return model.PodCgroupMemory{}, err
		}
		r.root = root
	}
	return cgroup.ReadPodCgroup(r.root, dir)
}

// close releases the cgroup root and returns the Pod cgroups that could not be
// read. Like an unreadable container file, each one is a read error of the
// scan.
func (r *podCgroupReader) close() error {
	if r.root != nil {
		_ = r.root.Close()
	}
	return errors.Join(r.errs...)
}

// cgroupNamesPod reports whether the Pod identity in a cgroup path is the
// mapped Pod. A static Pod's cgroup is named after its configuration hash,
// which the index maps to the mirror Pod's UID.
func cgroupNamesPod(index kube.PodIndex, cgroupPodUID, podUID string) bool {
	if podUID == "" || cgroupPodUID == "" {
		return false
	}
	return cgroupPodUID == podUID || index.PodUIDs[cgroupPodUID] == podUID
}
