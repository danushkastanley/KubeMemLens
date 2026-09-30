package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

type boundProcess struct {
	pid, fd           int
	start             uint64
	digest, container string
}

func procPath(pid int, name string) string { return "/proc/" + strconv.Itoa(pid) + "/" + name }
func boundedFile(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errObservation
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errObservation
	}
	return data, nil
}
func startTime(pid int) (uint64, error) {
	data, err := boundedFile(procPath(pid, "stat"), 4096)
	if err != nil {
		return 0, err
	}
	end := strings.LastIndex(string(data), ") ")
	if end < 0 {
		return 0, errObservation
	}
	fields := strings.Fields(string(data[end+2:]))
	if len(fields) < 20 {
		return 0, errObservation
	}
	n, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || n == 0 {
		return 0, errObservation
	}
	return n, nil
}
func openBound(pid int, start uint64, digest, container string) (*boundProcess, error) {
	if pid <= 1 || pid > 1<<31-1 || start == 0 || len(digest) != 64 || len(container) != 64 || strings.Trim(digest+container, "0123456789abcdef") != "" {
		return nil, errObservation
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return nil, errObservation
	}
	p := &boundProcess{pid, fd, start, digest, container}
	if p.verify() != nil {
		p.close()
		return nil, errObservation
	}
	return p, nil
}
func (p *boundProcess) alive() error {
	if unix.PidfdSendSignal(p.fd, 0, nil, 0) != nil {
		return errObservation
	}
	start, err := startTime(p.pid)
	if err != nil || start != p.start {
		return errObservation
	}
	return nil
}
func (p *boundProcess) verify() error {
	if p.alive() != nil {
		return errObservation
	}
	data, err := boundedFile(procPath(p.pid, "cgroup"), 4096)
	if err != nil {
		return err
	}
	path := strings.TrimSuffix(string(data), "\n")
	if strings.Contains(path, "\n") || !strings.HasPrefix(path, "0::/") {
		return errObservation
	}
	path = path[3:]
	name := filepath.Base(path)
	if filepath.Clean(path) != path || (name != p.container && name != "cri-containerd-"+p.container+".scope") {
		return errObservation
	}
	f, err := os.Open(procPath(p.pid, "exe"))
	if err != nil {
		return errObservation
	}
	info, statErr := f.Stat()
	h := sha256.New()
	n, readErr := io.Copy(h, io.LimitReader(f, 128<<20+1))
	closeErr := f.Close()
	if statErr != nil || !info.Mode().IsRegular() || n != info.Size() || n <= 0 || n > 128<<20 || readErr != nil || closeErr != nil || hex.EncodeToString(h.Sum(nil)) != p.digest || p.alive() != nil {
		return errObservation
	}
	return nil
}
func (p *boundProcess) close() error { return unix.Close(p.fd) }
