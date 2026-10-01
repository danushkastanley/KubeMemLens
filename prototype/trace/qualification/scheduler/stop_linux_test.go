package scheduler

import (
	"errors"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestStopRetainsDescriptorsAndClosePreservesStopFailure(t *testing.T) {
	source, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	fd, err := unix.Dup(int(source.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	capture := &Capture{fds: []int{fd}}
	// A regular descriptor rejects perf disable. Stop must still retain it for
	// orderly release, and Close must not erase that earlier integrity failure.
	if err := capture.Stop(); !errors.Is(err, ErrObservation) {
		t.Fatal("missing stop failure")
	}
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); err != nil {
		t.Fatal("Stop closed a descriptor")
	}
	if err := capture.Stop(); !errors.Is(err, ErrObservation) {
		t.Fatal("stop failure was cleared")
	}
	if err := capture.Close(); !errors.Is(err, ErrObservation) {
		t.Fatal("close hid stop failure")
	}
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); err != unix.EBADF {
		t.Fatal("Close retained descriptor")
	}
	if err := capture.Close(); !errors.Is(err, ErrObservation) {
		t.Fatal("close failure was cleared")
	}
}
