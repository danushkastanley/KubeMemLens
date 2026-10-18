package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestOwnerPinsTheProcessAndRejectsWrongExecutableBeforeProbes(t *testing.T) {
	child := exec.Command("/bin/sleep", "60")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	start, err := processStart(child.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	cfg := configurationFixture()
	cfg.Anchor = anchorConfiguration{PID: child.Process.Pid, Start: start, SHA256: strings.Repeat("0", 64)}
	if _, err := bindOwner(cfg); err == nil {
		t.Fatal("wrong executable accepted")
	}
	fd, err := unix.PidfdOpen(child.Process.Pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	guard := &ownerGuard{pidfd: os.NewFile(uintptr(fd), "owned-test-process")}
	defer func() {
		if err := guard.close(); err != nil {
			t.Error(err)
		}
	}()
	if guard.alive() != nil {
		t.Fatal("live owner unavailable")
	}
	if guard.verify() == nil {
		t.Fatal("incomplete executable/cgroup binding accepted")
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	if guard.alive() == nil {
		t.Fatal("dead owner remained eligible")
	}
}

func TestOwnerCloseIsIdempotentAndKeepsErrors(t *testing.T) {
	var descriptors [2]int
	if unix.Pipe2(descriptors[:], unix.O_CLOEXEC) != nil {
		t.Fatal("pipe failed")
	}
	guard := &ownerGuard{pidfd: os.NewFile(uintptr(descriptors[0]), "owned-test-process"), group: os.NewFile(uintptr(descriptors[1]), "owned-test-group")}
	if guard.pidfd.Close() != nil {
		t.Fatal("prepare failed close")
	}
	if guard.close() == nil || guard.close() == nil {
		t.Fatal("cleanup error disappeared")
	}
	if guard.alive() == nil {
		t.Fatal("closed owner reused")
	}
}
