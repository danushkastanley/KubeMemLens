//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestContainmentFormatRequiresExplicitConfiguration(t *testing.T) {
	for _, text := range []string{`{"seconds":1}`, `{"seconds":1,"observation":"containment"}`} {
		if _, err := loadConfiguration(strings.NewReader(text)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := loadConfiguration(strings.NewReader(`{"seconds":1,"observation":"unknown"}`)); err == nil {
		t.Fatal("unknown observation accepted")
	}
}

func TestContainmentReadsMissingPeakWithoutInventingZero(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{"cpu.max": "200000 100000\n", "cpu.max.burst": "0\n", "memory.max": "536870912\n", "pids.max": "max\n"}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	g := group{root: r, spec: groupSpec{Role: "node", Path: dir, Inode: info.Sys().(*syscall.Stat_t).Ino}}
	got, err := g.containment()
	if err != nil || got.MemoryPeak.State != "unavailable" || got.MemoryPeak.Value != nil || got.PIDsMax.State != "unlimited" {
		t.Fatal(got, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "memory.peak"), []byte("12345\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err = g.containment()
	if err != nil || *got.MemoryPeak.Value != 12345 {
		t.Fatal(got, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "memory.peak"), []byte("unknown\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := g.containment(); err == nil {
		t.Fatal("malformed counter became unavailable")
	}
	if err := os.WriteFile(filepath.Join(dir, "memory.peak"), []byte("12345\n"), 0600); err != nil {
		t.Fatal(err)
	}
	g.spec.Inode++
	if _, err := g.containment(); err == nil {
		t.Fatal("changed group binding accepted")
	}
}
