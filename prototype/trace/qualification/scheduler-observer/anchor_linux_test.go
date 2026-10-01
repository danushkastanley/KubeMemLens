package main

import (
	"os/exec"
	"testing"
)

func TestAnchorPinsExactLifetimeAndDetectsOwnedProcessExit(t *testing.T) {
	child := exec.Command("/bin/sleep", "60")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	start, err := processStart(child.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bindAnchor(anchorConfiguration{PID: child.Process.Pid, Start: start + 1}); err == nil {
		t.Fatal("wrong process lifetime accepted")
	}
	anchor, err := bindAnchor(anchorConfiguration{PID: child.Process.Pid, Start: start})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := anchor.close(); err != nil {
			t.Error(err)
		}
	}()
	if err := anchor.alive(); err != nil {
		t.Fatal(err)
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	if anchor.alive() == nil {
		t.Fatal("dead owner remained eligible")
	}
}

func TestAnchorRefusesUnboundedOrUnownedSelectors(t *testing.T) {
	for _, cfg := range []anchorConfiguration{{}, {PID: 1, Start: 1}, {PID: -1, Start: 1}, {PID: 2, Start: 0}} {
		if _, err := bindAnchor(cfg); err == nil {
			t.Fatal("invalid owner binding accepted")
		}
	}
}
