package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestSyscallObserverStopsAndReapsVerifiedChild(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
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
	cgroup, err := os.Open("/sys/fs/cgroup")
	if err != nil {
		t.Fatal(err)
	}
	defer cgroup.Close()
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path)
	cmd.Args = []string{"memlens-filecache-worker"}
	cmd.Env = []string{"GOTRACEBACK=none"}
	cmd.Stdin = input
	cmd.ExtraFiles = []*os.File{cgroup}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		cancel()
		if !waited {
			_ = cmd.Wait()
		}
	}()
	worker, err := openProcess(cmd.Process.Pid, digest, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer worker.close()
	tracer := &taskTracer{workers: []*process{worker}, tasks: map[int]*tracedTask{}}
	defer func() { _ = tracer.finish() }()
	if err := tracer.stopAll(); err != nil {
		t.Fatal("could not stop verified fixture threads", err)
	}
	if len(tracer.tasks) == 0 {
		t.Fatal("no owned threads observed")
	}
	for tid := range tracer.tasks {
		if err := tracer.resume(tid, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := tracer.stopAll(); err != nil {
		t.Fatal("could not stop resumed fixture threads", err)
	}
	if err := tracer.finish(); err != nil {
		t.Fatal("could not drain owned trace exits", err)
	}
	waited = true
	if err := cmd.Wait(); err == nil {
		t.Fatal("owned child survived explicit fault termination")
	}
	if len(tracer.tasks) != 0 || unix.PidfdSendSignal(worker.fd, 0, nil, 0) == nil {
		t.Fatal("owned tracee was not reaped")
	}
}
