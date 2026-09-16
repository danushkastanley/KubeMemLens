package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

var errOwnership = errors.New("qualified process ownership unavailable")

type process struct {
	pid, fd int
	start   uint64
}

func readBounded(path string, maximum int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(f, maximum+1))
	closeErr := f.Close()
	if readErr != nil || closeErr != nil || int64(len(data)) > maximum {
		return nil, errOwnership
	}
	return data, nil
}

func procPath(pid int, name string) string { return "/proc/" + strconv.Itoa(pid) + "/" + name }

func startTime(data []byte) (uint64, error) {
	// comm can contain spaces and parentheses; fields after its final ')' have
	// the fixed proc(5) layout. Field 22 is starttime, not a wall-clock timestamp.
	end := strings.LastIndex(string(data), ") ")
	if end < 0 {
		return 0, errOwnership
	}
	fields := strings.Fields(string(data[end+2:]))
	if len(fields) < 20 {
		return 0, errOwnership
	}
	value, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || value == 0 {
		return 0, errOwnership
	}
	return value, nil
}

func openProcess(pid int, digest string, expectedStart uint64) (*process, error) {
	if pid <= 1 || pid > 1<<31-1 || len(digest) != 64 || strings.Trim(digest, "0123456789abcdef") != "" {
		return nil, errOwnership
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return nil, errOwnership
	}
	p := &process{pid: pid, fd: fd}
	accepted := false
	defer func() {
		if !accepted {
			_ = p.close()
		}
	}()
	stat, err := readBounded(procPath(pid, "stat"), 4096)
	if err != nil {
		return nil, errOwnership
	}
	p.start, err = startTime(stat)
	if err != nil || (expectedStart != 0 && p.start != expectedStart) {
		return nil, errOwnership
	}
	f, err := os.Open(procPath(pid, "exe"))
	if err != nil {
		return nil, errOwnership
	}
	info, statErr := f.Stat()
	hash := sha256.New()
	n, copyErr := io.Copy(hash, io.LimitReader(f, (128<<20)+1))
	closeErr := f.Close()
	if statErr != nil || !info.Mode().IsRegular() || n != info.Size() || n <= 0 || n > 128<<20 || copyErr != nil || closeErr != nil || hex.EncodeToString(hash.Sum(nil)) != digest || p.check() != nil {
		return nil, errOwnership
	}
	accepted = true
	return p, nil
}

func (p *process) check() error {
	if unix.PidfdSendSignal(p.fd, 0, nil, 0) != nil {
		return errOwnership
	}
	data, err := readBounded(procPath(p.pid, "stat"), 4096)
	if err != nil {
		return errOwnership
	}
	start, err := startTime(data)
	if err != nil || start != p.start {
		return errOwnership
	}
	return nil
}

func (p *process) close() error { return unix.Close(p.fd) }

func containerMembership(data []byte, id string) (string, error) {
	if len(id) != 64 || strings.Trim(id, "0123456789abcdef") != "" {
		return "", errOwnership
	}
	line := strings.TrimSuffix(string(data), "\n")
	if strings.Contains(line, "\n") || !strings.HasPrefix(line, "0::/") {
		return "", errOwnership
	}
	path := strings.TrimPrefix(line, "0::")
	name := filepath.Base(path)
	if filepath.Clean(path) != path || (name != id && name != "cri-containerd-"+id+".scope") {
		return "", errOwnership
	}
	return path, nil
}

func (p *process) containerPath(id string) (string, error) {
	data, err := readBounded(procPath(p.pid, "cgroup"), 4096)
	if err != nil {
		return "", errOwnership
	}
	path, err := containerMembership(data, id)
	if err != nil || p.check() != nil {
		return "", errOwnership
	}
	return path, nil
}

func field(data []byte, key string) string {
	for _, line := range strings.Split(string(data), "\n") {
		name, value, ok := strings.Cut(line, ":")
		if ok && name == key {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func children(parent *process) ([]int, error) {
	threads, err := os.ReadDir(procPath(parent.pid, "task"))
	if err != nil || len(threads) > 256 {
		return nil, errOwnership
	}
	seen := map[int]bool{}
	for _, thread := range threads {
		data, err := readBounded(procPath(parent.pid, "task/"+thread.Name()+"/children"), 4096)
		if errors.Is(err, os.ErrNotExist) {
			continue // A runtime thread may finish during the snapshot.
		}
		if err != nil {
			return nil, errOwnership
		}
		for _, value := range strings.Fields(string(data)) {
			pid, err := strconv.Atoi(value)
			if err != nil || pid <= 1 || len(seen) >= 32 {
				return nil, errOwnership
			}
			seen[pid] = true
		}
	}
	var result []int
	for pid := range seen {
		result = append(result, pid)
	}
	return result, parent.check()
}
