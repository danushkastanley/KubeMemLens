//go:build linux

package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type groupSpec struct {
	Role  string `json:"role"`
	Path  string `json:"path"`
	Inode uint64 `json:"inode"`
}

type group struct {
	spec groupSpec
	root *os.Root
}

type groupSample struct {
	CPU           map[string]uint64 `json:"cpu"`
	MemoryCurrent uint64            `json:"memoryCurrent"`
	Memory        map[string]uint64 `json:"memory"`
	MemoryEvents  map[string]uint64 `json:"memoryEvents"`
	RSSBytes      uint64            `json:"rssBytes"`
	Processes     int               `json:"processes"`
	PIDs          uint64            `json:"pids"`
}

func readBounded(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, 65537))
	if err != nil || len(b) > 65536 {
		return nil, errors.New("counter read failed")
	}
	return b, nil
}

func (g *group) read(name string) ([]byte, error) {
	f, err := g.root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readBounded(f)
}

func openGroup(spec groupSpec) (*group, error) {
	if (spec.Role != "node" && spec.Role != "api") || spec.Inode == 0 ||
		filepath.Clean(spec.Path) != spec.Path || !strings.HasPrefix(spec.Path, "/sys/fs/cgroup/") {
		return nil, errors.New("invalid group binding")
	}
	r, err := os.OpenRoot(spec.Path)
	if err != nil {
		return nil, err
	}
	g := &group{spec, r}
	if err := g.validate(); err != nil {
		r.Close()
		return nil, err
	}
	return g, nil
}

func (g *group) validate() error {
	for _, read := range []func() (os.FileInfo, error){
		func() (os.FileInfo, error) { return g.root.Stat(".") },
		func() (os.FileInfo, error) { return os.Lstat(g.spec.Path) },
	} {
		info, err := read()
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.IsDir() || stat.Ino != g.spec.Inode {
			return errors.New("group lifetime changed")
		}
	}
	return nil
}

func counters(data []byte) (map[string]uint64, error) {
	fields := strings.Fields(string(data))
	if len(fields) == 0 || len(fields)%2 != 0 {
		return nil, errors.New("invalid counters")
	}
	out := map[string]uint64{}
	for i := 0; i < len(fields); i += 2 {
		if _, exists := out[fields[i]]; exists {
			return nil, errors.New("duplicate counter")
		}
		v, err := strconv.ParseUint(fields[i+1], 10, 64)
		if err != nil {
			return nil, err
		}
		out[fields[i]] = v
	}
	return out, nil
}

func (g *group) sample() (groupSample, error) {
	var s groupSample
	if err := g.validate(); err != nil {
		return s, err
	}
	for _, entry := range []struct {
		name   string
		target *map[string]uint64
	}{
		{"cpu.stat", &s.CPU}, {"memory.stat", &s.Memory}, {"memory.events", &s.MemoryEvents},
	} {
		b, err := g.read(entry.name)
		if err != nil {
			return s, err
		}
		*entry.target, err = counters(b)
		if err != nil {
			return s, err
		}
	}
	for _, entry := range []struct {
		name   string
		target *uint64
	}{
		{"memory.current", &s.MemoryCurrent}, {"pids.current", &s.PIDs},
	} {
		b, err := g.read(entry.name)
		if err != nil {
			return s, err
		}
		*entry.target, err = strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
		if err != nil {
			return s, err
		}
	}
	var err error
	s.RSSBytes, s.Processes, err = g.resident()
	if err != nil {
		return s, err
	}
	return s, g.validate()
}
