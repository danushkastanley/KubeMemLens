package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestMain(m *testing.M) {
	if os.Args[0] == "memlens-filecache-worker" {
		_, err := io.Copy(io.Discard, os.Stdin)
		if err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestChildSelectionUsesExactCgroupAndExecutable(t *testing.T) {
	cgroup, err := os.Open("/sys/fs/cgroup")
	if err != nil {
		t.Skip("requires a cgroup v2 test environment")
	}
	defer cgroup.Close()
	var stat unix.Stat_t
	var fs unix.Statfs_t
	if unix.Fstatfs(int(cgroup.Fd()), &fs) != nil || fs.Type != unix.CGROUP2_SUPER_MAGIC || unix.Fstat(int(cgroup.Fd()), &stat) != nil {
		t.Skip("requires a cgroup v2 test environment")
	}
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	parent, err := openProcess(os.Getpid(), digest, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	defer writer.Close()
	cmd := exec.CommandContext(ctx, path)
	cmd.Args = []string{"memlens-filecache-worker"}
	cmd.Env = []string{"GOTRACEBACK=none"}
	cmd.Stdin = input
	cmd.ExtraFiles = []*os.File{cgroup}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = cmd.Wait() }()
	workers, excluded, err := ownedChildren(parent, digest, map[uint64]bool{stat.Ino: true})
	if err != nil || len(workers) != 1 || excluded != 0 {
		t.Fatal("exact child ownership was not established")
	}
	defer workers[0].close()
	wrong, excluded, err := ownedChildren(parent, digest, map[uint64]bool{stat.Ino + 1: true})
	if err != nil || len(wrong) != 0 || excluded != 1 || workers[0].check() != nil {
		t.Fatal("non-selected worker was returned or modified")
	}
	var output bytes.Buffer
	if signalOwned(json.NewEncoder(&output), parent, workers, 1, "KILL", "workers") == nil || output.Len() != 0 || workers[0].check() != nil {
		t.Fatal("uncertain additional children permitted signalling")
	}
	value, err := inspectWorkers(workers, 0)
	if err != nil || value.Workers != 1 || value.ActiveControls != 0 || len(value.Objects["map"]) != 0 || len(value.Objects["prog"]) != 0 || len(value.Objects["link"]) != 0 {
		t.Fatal("capless child acquired invented BPF objects")
	}
}
