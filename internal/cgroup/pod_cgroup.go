package cgroup

import (
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/model"
)

// PodCgroupDir returns the Pod cgroup that holds a container cgroup: the
// path up to and including the component that names the Pod UID, found with
// the same pattern as ExtractPodUIDFromPath. It is not the parent directory:
// a container cgroup can sit deeper, and a Kubernetes node that is itself a
// container puts its own scope above the Pod. The first Pod component wins,
// so a Pod nested inside a container belongs to the outer Pod.
func PodCgroupDir(relativePath string) (string, bool) {
	location := podUIDPattern.FindStringIndex(relativePath)
	if location == nil {
		return "", false
	}
	end := strings.IndexByte(relativePath[location[0]:], '/')
	if end < 0 {
		return relativePath, true
	}
	return relativePath[:location[0]+end], true
}

// ReadPodCgroup reads memory.current, memory.max and memory.events.local of
// the Pod cgroup at dir below root. The directory is opened through root, so a
// walked name cannot resolve outside the cgroup tree, and each file is read
// with the same bound as a container's files.
//
// A missing file leaves its value unknown. A Pod cgroup that disappears
// between the walk and this read is unknown as a whole, not an error: there is
// nothing left to read. Any other failure is a live cgroup that could not be
// read and is returned.
func ReadPodCgroup(root *os.Root, dir string) (model.PodCgroupMemory, error) {
	podRoot, err := root.OpenRoot(dir)
	if cgroupGone(err) {
		return model.PodCgroupMemory{}, nil
	}
	if err != nil {
		return model.PodCgroupMemory{}, fmt.Errorf("open Pod cgroup: %w", err)
	}
	defer podRoot.Close()
	return readPodCgroupFS(podRoot.FS())
}

func readPodCgroupFS(fsys fs.FS) (model.PodCgroupMemory, error) {
	pod, err := readPodCgroupFiles(fsys)
	if cgroupGone(err) {
		return model.PodCgroupMemory{}, nil
	}
	return pod, err
}

func readPodCgroupFiles(fsys fs.FS) (model.PodCgroupMemory, error) {
	var pod model.PodCgroupMemory
	var err error
	if pod.CurrentBytes, pod.CurrentKnown, _, err = readOptionalScalar(fsys, "memory.current", false); err != nil {
		return model.PodCgroupMemory{}, err
	}
	if pod.MaxBytes, pod.MaxKnown, pod.MaxUnlimited, err = readOptionalScalar(fsys, "memory.max", true); err != nil {
		return model.PodCgroupMemory{}, err
	}
	local, known, err := readOptionalEvents(fsys, "memory.events.local", knownEventKeys)
	if err != nil {
		return model.PodCgroupMemory{}, err
	}
	pod.LocalEventsKnown = known
	pod.LocalOOMEvents = local["oom"]
	pod.LocalOOMKillEvents = local["oom_kill"]
	pod.LocalMaxEvents = local["max"]
	return pod, nil
}
