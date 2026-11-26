package cgroup

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// The cgroup hierarchy a node uses, as the agent reports it in
// api.NodeEnvironment.CgroupVersion. VersionUnknown means it could not be
// told.
const (
	VersionV2      = "v2"
	VersionV1      = "v1"
	VersionUnknown = ""
)

// DetectVersion reports the cgroup version of the tree mounted at root. A
// unified (v2) root has a cgroup.controllers file. A v1 root, and a hybrid root
// whose memory controller stays on v1, has a memory controller directory
// instead. Anything else is unknown: the agent never assumes v2.
//
// The root is opened as an os.Root, so no name it checks can resolve outside
// it. A failure other than a missing name returns VersionUnknown with the
// error; it is never read as v1.
func DetectVersion(root string) (string, error) {
	dir, err := os.OpenRoot(root)
	if err != nil {
		return VersionUnknown, fmt.Errorf("open cgroup root: %w", err)
	}
	defer dir.Close()
	return detectVersionFS(dir.FS())
}

func detectVersionFS(fsys fs.FS) (string, error) {
	controllers, err := statIfExists(fsys, "cgroup.controllers")
	if err != nil {
		return VersionUnknown, err
	}
	if controllers != nil && controllers.Mode().IsRegular() {
		return VersionV2, nil
	}
	memory, err := statIfExists(fsys, "memory")
	if err != nil {
		return VersionUnknown, err
	}
	if memory != nil && memory.IsDir() {
		return VersionV1, nil
	}
	return VersionUnknown, nil
}

// statIfExists returns nil, nil for a name that does not exist.
func statIfExists(fsys fs.FS, name string) (fs.FileInfo, error) {
	info, err := fs.Stat(fsys, name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", name, err)
	}
	return info, nil
}
