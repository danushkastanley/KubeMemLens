package cgroup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/danushkastanley/kube-memlens/internal/model"
)

type Entry struct {
	Path         string
	RelativePath string
	PodUID       string
	ContainerID  string
	Memory       model.MemoryBreakdown
}

type Walker struct {
	Root string

	// parse reads one container cgroup. Tests replace it to reproduce a
	// cgroup that changes between being listed and being read.
	parse func(name, dir string) (model.MemoryBreakdown, error)
}

func (w Walker) Walk() ([]Entry, error) {
	return w.WalkContext(context.Background())
}

func (w Walker) WalkContext(ctx context.Context) ([]Entry, error) {
	if strings.TrimSpace(w.Root) == "" {
		return nil, errors.New("cgroup root is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	root := filepath.Clean(w.Root)
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat cgroup root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("cgroup root is not a directory: %s", w.Root)
	}

	parse := w.parse
	if parse == nil {
		parse = ParseDirectory
	}
	var entries []Entry
	var errs []error
	containerDirs := map[string]string{}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			if !cgroupGone(walkErr) {
				errs = append(errs, fmt.Errorf("walk %s: %w", path, walkErr))
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if !hasMemoryFiles(path) {
			return nil
		}

		containerID := ExtractContainerIDFromPath(path)
		if containerID == "" {
			return nil
		}
		// cgroup v2 is hierarchical: a sub-cgroup created inside a container
		// (systemd, Docker-in-Docker) is already charged to the container.
		if nestedInContainer(containerDirs, root, path, containerID) {
			return nil
		}
		containerDirs[path] = containerID

		relativePath, err := filepath.Rel(root, path)
		if err != nil {
			relativePath = path
		}

		breakdown, err := parse(containerID, path)
		if err != nil {
			if !cgroupGone(err) {
				errs = append(errs, fmt.Errorf("parse %s: %w", path, err))
			}
			return nil
		}

		entries = append(entries, Entry{
			Path:         path,
			RelativePath: relativePath,
			PodUID:       ExtractPodUIDFromPath(path),
			ContainerID:  containerID,
			Memory:       breakdown,
		})
		return nil
	})
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return entries, err
	}
	if err != nil {
		errs = append(errs, err)
	}

	return entries, errors.Join(errs...)
}

// cgroupGone reports whether an error means the cgroup was removed while the
// walk was in progress. Containers and systemd scopes stop all the time, so a
// cgroup that no longer exists is not a gap in the evidence: there is nothing
// left to read. The memory files were present when the directory was listed,
// so a missing file means the cgroup went away, and the kernel returns ENODEV
// for a read through a handle that outlived its cgroup. Any other failure is
// a live cgroup that could not be read and still fails the scan.
func cgroupGone(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENODEV)
}

func ExtractPodUIDFromPath(path string) string {
	match := podUIDPattern.FindStringSubmatch(path)
	if len(match) != 2 {
		return ""
	}

	return strings.ToLower(strings.ReplaceAll(match[1], "_", "-"))
}

func ExtractContainerIDFromPath(path string) string {
	var longest string
	for _, part := range splitPath(path) {
		part = strings.TrimSuffix(part, ".scope")
		for _, prefix := range []string{"cri-containerd-", "docker-", "crio-"} {
			if strings.HasPrefix(part, prefix) {
				id := strings.TrimPrefix(part, prefix)
				if isHexID(id, 12, 128) && len(id) > len(longest) {
					longest = strings.ToLower(id)
				}
			}
		}

		if isHexID(part, 12, 128) && len(part) > len(longest) {
			longest = strings.ToLower(part)
		}
	}

	return longest
}

func nestedInContainer(containerDirs map[string]string, root, path, containerID string) bool {
	for dir := path; dir != root && dir != filepath.Dir(dir); {
		dir = filepath.Dir(dir)
		if containerDirs[dir] == containerID {
			return true
		}
	}
	return false
}

func hasMemoryFiles(dir string) bool {
	for _, name := range []string{"memory.current", "memory.stat"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.IsDir() {
			return false
		}
	}
	return true
}

func splitPath(path string) []string {
	return strings.FieldsFunc(path, func(r rune) bool {
		return r == '/' || r == '\\'
	})
}

func isHexID(value string, minLength, maxLength int) bool {
	if len(value) < minLength || len(value) > maxLength {
		return false
	}
	for _, r := range value {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			continue
		}
		return false
	}
	return true
}

var podUIDPattern = regexp.MustCompile(`pod([0-9a-fA-F]{32}|[0-9a-fA-F]{8}[-_][0-9a-fA-F]{4}[-_][0-9a-fA-F]{4}[-_][0-9a-fA-F]{4}[-_][0-9a-fA-F]{12})(?:\.slice|/|$)`)
