package main

import (
	"errors"
	"golang.org/x/sys/unix"
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

func TestAnchorRetriesOnlyInterruptedPollsAndStillRequiresLiveness(t *testing.T) {
	for _, tc := range []struct {
		name       string
		interrupts int
		count      int
		revents    int16
		pollError  error
		want       error
		calls      int
	}{
		{name: "live after interruption", interrupts: 1, calls: 2},
		{name: "exit after interruption", interrupts: 1, count: 1, revents: unix.POLLIN, want: errAnchor, calls: 2},
		{name: "closed descriptor", revents: unix.POLLNVAL, count: 1, want: errAnchor, calls: 1},
		{name: "poll failure", pollError: unix.EBADF, want: errAnchorPoll, calls: 1},
		{name: "bounded repeated signals", interrupts: 3, want: errAnchorInterrupted, calls: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			err := pollAnchor(42, func(fds []unix.PollFd, timeout int) (int, error) {
				calls++
				if timeout != 0 || len(fds) != 1 || fds[0].Fd != 42 || fds[0].Events != unix.POLLIN {
					t.Fatal("poll broadened beyond the owned pidfd")
				}
				if calls <= tc.interrupts {
					return -1, unix.EINTR
				}
				fds[0].Revents = tc.revents
				return tc.count, tc.pollError
			})
			if !errors.Is(err, tc.want) || calls != tc.calls {
				t.Fatalf("result %v after %d calls; want %v after %d", err, calls, tc.want, tc.calls)
			}
		})
	}
}
