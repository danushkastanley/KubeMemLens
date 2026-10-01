package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

var errOwner = errors.New("verifier owner binding changed or exited")

type ownerGuard struct {
	pidfd, executable, group *os.File
	cfg                      configuration
	stat                     unix.Stat_t
	closed                   bool
	closeErr                 error
}

func processStart(pid int) (uint64, error) {
	raw, err := boundedRead("/proc/"+strconv.Itoa(pid)+"/stat", 4096, kernelData)
	if err != nil {
		return 0, errOwner
	}
	end := strings.LastIndex(string(raw), ") ")
	if end < 0 {
		return 0, errOwner
	}
	fields := strings.Fields(string(raw[end+2:]))
	if len(fields) < 20 {
		return 0, errOwner
	}
	value, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || value == 0 {
		return 0, errOwner
	}
	return value, nil
}

func bindOwner(cfg configuration) (guard *ownerGuard, err error) {
	if cfg.Anchor.PID <= 1 || cfg.Anchor.Start == 0 {
		return nil, errOwner
	}
	p := &ownerGuard{cfg: cfg}
	defer func() {
		if err != nil {
			err = errors.Join(err, p.close())
		}
	}()
	fd, err := unix.PidfdOpen(cfg.Anchor.PID, 0)
	if err != nil {
		return nil, errOwner
	}
	p.pidfd = os.NewFile(uintptr(fd), "owned-verifier-process")
	start, err := processStart(cfg.Anchor.PID)
	if err != nil || start != cfg.Anchor.Start {
		return nil, errOwner
	}
	fd, err = unix.Open("/proc/"+strconv.Itoa(cfg.Anchor.PID)+"/exe", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errOwner
	}
	p.executable = os.NewFile(uintptr(fd), "owned-verifier-executable")
	if unix.Fstat(fd, &p.stat) != nil || p.stat.Mode&unix.S_IFMT != unix.S_IFREG || p.stat.Size <= 0 || p.stat.Size > 128<<20 {
		return nil, errOwner
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(p.executable, 128<<20+1))
	if err != nil || n != p.stat.Size || hex.EncodeToString(hash.Sum(nil)) != cfg.Anchor.SHA256 {
		return nil, errOwner
	}
	fd, err = unix.Open(cfg.Group.Path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, errOwner
	}
	p.group = os.NewFile(uintptr(fd), "owned-verifier-cgroup")
	if err = p.verify(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *ownerGuard) alive() error {
	if p.closed || p.pidfd == nil {
		return errOwner
	}
	for attempt := 0; attempt < 3; attempt++ {
		fds := []unix.PollFd{{Fd: int32(p.pidfd.Fd()), Events: unix.POLLIN}}
		count, err := unix.Poll(fds, 0)
		if err == unix.EINTR {
			continue
		}
		if err != nil || count != 0 || fds[0].Revents != 0 {
			return errOwner
		}
		return nil
	}
	return errOwner
}

func (p *ownerGuard) verify() error {
	if p.alive() != nil || p.group == nil || p.executable == nil {
		return errOwner
	}
	var current, group unix.Stat_t
	var fs unix.Statfs_t
	if unix.Stat("/proc/"+strconv.Itoa(p.cfg.Anchor.PID)+"/exe", &current) != nil ||
		current.Dev != p.stat.Dev || current.Ino != p.stat.Ino || current.Size != p.stat.Size || current.Mtim != p.stat.Mtim || current.Ctim != p.stat.Ctim {
		return errOwner
	}
	if unix.Fstat(int(p.group.Fd()), &group) != nil || unix.Fstatfs(int(p.group.Fd()), &fs) != nil ||
		fs.Type != unix.CGROUP2_SUPER_MAGIC || group.Ino != p.cfg.Group.Inode || group.Nlink == 0 {
		return errOwner
	}
	raw, err := boundedRead("/proc/"+strconv.Itoa(p.cfg.Anchor.PID)+"/cgroup", 4096, kernelData)
	if err != nil {
		return errOwner
	}
	value := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(value, "0::/") || strings.Contains(value, "\n") || "/sys/fs/cgroup"+strings.TrimPrefix(value, "0::") != p.cfg.Group.Path {
		return errOwner
	}
	return p.alive()
}

func (p *ownerGuard) close() error {
	if p.closed {
		return p.closeErr
	}
	p.closed = true
	for _, file := range []*os.File{p.group, p.executable, p.pidfd} {
		if file != nil {
			p.closeErr = errors.Join(p.closeErr, file.Close())
		}
	}
	return p.closeErr
}
