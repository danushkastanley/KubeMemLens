//go:build linux && (amd64 || arm64)

package workercontainment

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/prototype/trace/internal/testworker"
	"golang.org/x/sys/unix"
)

func TestContainmentFixture(t *testing.T) {
	mode := os.Getenv("KML_CONTAINMENT_FIXTURE")
	if mode == "" {
		return
	}
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM, 0)
	if err != nil {
		os.Exit(30)
	}
	_ = unix.Close(fd)
	baseline, err := unix.PrctlRetInt(unix.PR_GET_SECCOMP, 0, 0, 0, 0)
	if err != nil {
		os.Exit(40)
	}
	if baseline == 0 && Restrict() != ErrContainment {
		os.Exit(41)
	}
	if testworker.Baseline() != nil {
		os.Exit(42)
	}
	start, ready, done := make(chan struct{}), make(chan struct{}, 8), make(chan bool, 8)
	// Existing locked OS threads must receive the synchronised filter too.
	for range 8 {
		go func() {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			ready <- struct{}{}
			<-start
			fd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM, 0)
			if err == nil {
				_ = unix.Close(fd)
			}
			done <- err == unix.EPERM
		}()
	}
	for range 8 {
		<-ready
	}
	if Restrict() != nil || testworker.CheckBaseline() != nil {
		os.Exit(31)
	}
	dumpable, err := unix.PrctlRetInt(unix.PR_GET_DUMPABLE, 0, 0, 0, 0)
	var limit unix.Rlimit
	if err != nil || dumpable != 0 || unix.Getrlimit(unix.RLIMIT_CORE, &limit) != nil || limit.Cur != 0 || limit.Max != 0 {
		os.Exit(39)
	}
	close(start)
	for range 8 {
		if !<-done {
			os.Exit(32)
		}
	}
	if mode == "wrong-abi" {
		_, _, _ = unix.RawSyscall(0x40000000+unix.SYS_GETPID, 0, 0, 0)
		os.Exit(33)
	}
	_, _, errno := unix.RawSyscall(unix.SYS_CLONE3, 0, 0, 0)
	if errno != unix.ENOSYS {
		os.Exit(34)
	}
	pid, _, errno := unix.RawSyscall(unix.SYS_CLONE, uintptr(unix.SIGCHLD), 0, 0)
	if errno == 0 && pid == 0 {
		_, _, _ = unix.RawSyscall(unix.SYS_EXIT, 35, 0, 0)
	}
	if errno != unix.EPERM {
		os.Exit(36)
	}
	if unix.Exec("/nonexistent", []string{"fixture"}, nil) != unix.EPERM {
		os.Exit(37)
	}
	fd, err = unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err == nil {
		_ = unix.Close(fd)
	}
	if err != unix.EPERM {
		os.Exit(38)
	}
	// Force fresh Go threads after restriction, with no ability to fork.
	threadReady, release := make(chan struct{}, 16), make(chan struct{})
	for range 16 {
		go func() { runtime.LockOSThread(); defer runtime.UnlockOSThread(); threadReady <- struct{}{}; <-release }()
	}
	for range 16 {
		<-threadReady
	}
	close(release)
	os.Exit(0)
}

func TestRealKernelRestrictionAndThreadSynchronisation(t *testing.T) {
	inherited, err := unix.PrctlRetInt(unix.PR_GET_SECCOMP, 0, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("inherited seccomp mode: %d", inherited)
	path, err := testworker.StaticBinary()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"normal", "wrong-abi"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, path, "-test.run=^TestContainmentFixture$")
			cmd.Env = []string{"KML_CONTAINMENT_FIXTURE=" + mode}
			diagnostics := &testworker.Diagnostics{}
			cmd.Stderr = diagnostics
			err := cmd.Run()
			if mode == "normal" {
				if err != nil {
					t.Fatalf("restriction fixture failed: %v: %s", err, diagnostics.String())
				}
				return
			}
			if cmd.ProcessState == nil {
				t.Fatalf("ABI fixture did not start: %v", err)
			}
			status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
			if err == nil || !ok || !status.Signaled() || status.Signal() != syscall.SIGSYS {
				t.Fatal("alternate ABI bypassed syscall policy")
			}
		})
	}
}
