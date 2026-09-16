package main

import (
	"errors"
	"os"
	"sort"
	"strconv"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"golang.org/x/sys/unix"
)

type objectIDs map[string][]uint32

type snapshot struct {
	Workers        int       `json:"workers"`
	Excluded       int       `json:"excludedWorkers"`
	ActiveControls int       `json:"activeControls"`
	Objects        objectIDs `json:"objects"`
	KernelMapBytes uint64    `json:"kernelMapBytes"`
	UserMapBytes   uint64    `json:"userMapBytes"`
	Clock          clockPair `json:"clock"`
}

func selectedTarget(pid int, targets map[uint64]bool) bool {
	path := procPath(pid, "fd/3")
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return false
	}
	var fs unix.Statfs_t
	var value unix.Stat_t
	fsErr, statErr := unix.Fstatfs(fd, &fs), unix.Fstat(fd, &value)
	closeErr := unix.Close(fd)
	return fsErr == nil && statErr == nil && closeErr == nil && fs.Type == unix.CGROUP2_SUPER_MAGIC && targets[value.Ino]
}

func ownedChildren(parent *process, digest string, targets map[uint64]bool) ([]*process, int, error) {
	return ownedChildrenWithOpen(parent, targets, func(pid int) (*process, error) { return openProcess(pid, digest, 0) })
}

func ownedChildrenWithOpen(parent *process, targets map[uint64]bool, openChild func(int) (*process, error)) (owned []*process, excluded int, resultErr error) {
	defer func() {
		if resultErr != nil {
			for _, child := range owned {
				_ = child.close()
			}
		}
	}()
	ids, err := children(parent)
	if err != nil {
		return nil, 0, err
	}
	for _, pid := range ids {
		if !selectedTarget(pid, targets) {
			excluded++
			continue
		}
		command, err := readBounded(procPath(pid, "cmdline"), 256)
		if err != nil || string(command) != "memlens-filecache-worker\x00" {
			excluded++
			continue
		}
		child, err := openChild(pid)
		if err != nil {
			return owned, excluded, err
		}
		owned = append(owned, child)
		status, err := readBounded(procPath(pid, "status"), 16384)
		if err != nil || field(status, "PPid") != strconv.Itoa(parent.pid) || !selectedTarget(pid, targets) || child.check() != nil || len(owned) > 2 {
			return owned, excluded, errOwnership
		}
	}
	return owned, excluded, parent.check()
}

func inspectWorkers(workers []*process, excluded int) (snapshot, error) {
	result := snapshot{Workers: len(workers), Excluded: excluded, Objects: objectIDs{"map": {}, "prog": {}, "link": {}}}
	seen := map[string]map[uint32]bool{"map": {}, "prog": {}, "link": {}}
	for _, child := range workers {
		path := procPath(child.pid, "fdinfo")
		files, err := os.ReadDir(path)
		if err != nil || len(files) > 256 {
			return snapshot{}, errOwnership
		}
		for _, file := range files {
			data, err := readBounded(path+"/"+file.Name(), 16384)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return snapshot{}, errOwnership
			}
			for _, kind := range []string{"link", "map", "prog"} {
				text := field(data, kind+"_id")
				if text == "" {
					continue
				}
				id, err := strconv.ParseUint(text, 10, 32)
				if err != nil || id == 0 {
					return snapshot{}, errOwnership
				}
				seen[kind][uint32(id)] = true
				break
			}
		}
		if child.check() != nil {
			return snapshot{}, errOwnership
		}
	}
	for kind, ids := range seen {
		for id := range ids {
			result.Objects[kind] = append(result.Objects[kind], id)
		}
		sort.Slice(result.Objects[kind], func(i, j int) bool { return result.Objects[kind][i] < result.Objects[kind][j] })
	}
	for _, id := range result.Objects["map"] {
		active, memory, err := activeControl(id)
		if err != nil {
			return snapshot{}, err
		}
		if active {
			result.ActiveControls++
		}
		result.KernelMapBytes += memory
	}
	// Reserve the same two ring mappings and four pages as the accepted
	// worker, independently of how much virtual memory is currently resident.
	result.UserMapBytes = uint64(len(workers)) * uint64(2*262144+4*os.Getpagesize())
	var err error
	result.Clock, err = readClock()
	return result, err
}

func activeControl(id uint32) (bool, uint64, error) {
	m, err := ebpf.NewMapFromID(ebpf.MapID(id))
	if err != nil {
		return false, 0, errOwnership
	}
	info, infoErr := m.Info()
	active := false
	var memory uint64
	if infoErr == nil {
		var known bool
		memory, known = info.Memlock()
		if !known || memory == 0 {
			infoErr = errOwnership
		}
	}
	if infoErr == nil && info.Name == "control" {
		if info.Type != ebpf.Array || info.KeySize != 4 || info.ValueSize != 4 || info.MaxEntries != 1 {
			infoErr = errOwnership
		} else {
			var key, value uint32
			infoErr = m.Lookup(&key, &value)
			active = value == 1
		}
	}
	if closeErr := m.Close(); infoErr != nil || closeErr != nil {
		return false, 0, errOwnership
	}
	return active, memory, nil
}

func remaining(ids objectIDs) (map[string]int, error) {
	counts := map[string]int{"map": 0, "prog": 0, "link": 0}
	if len(ids) != len(counts) {
		return nil, errOwnership
	}
	for kind, values := range ids {
		if _, known := counts[kind]; !known || values == nil || len(values) > 512 {
			return nil, errOwnership
		}
		seen := map[uint32]bool{}
		for _, id := range values {
			if id == 0 || seen[id] {
				return nil, errOwnership
			}
			seen[id] = true
			var err, closeErr error
			switch kind {
			case "map":
				var value *ebpf.Map
				value, err = ebpf.NewMapFromID(ebpf.MapID(id))
				if err == nil {
					closeErr = value.Close()
				}
			case "prog":
				var value *ebpf.Program
				value, err = ebpf.NewProgramFromID(ebpf.ProgramID(id))
				if err == nil {
					closeErr = value.Close()
				}
			case "link":
				var value link.Link
				value, err = link.NewFromID(link.ID(id))
				if err == nil {
					closeErr = value.Close()
				}
			default:
				return nil, errOwnership
			}
			if closeErr != nil || (err != nil && !errors.Is(err, os.ErrNotExist)) {
				return nil, errOwnership
			}
			if err == nil {
				counts[kind]++
			}
		}
	}
	return counts, nil
}
