package main

import (
	"context"
	"errors"
	"math"
	"os"

	"golang.org/x/sys/unix"
)

var errCompletion = errors.New("scheduler completion signal invalid")

// EOF is an explicit controller acknowledgement. Polling keeps cancellation
// responsive without a blocked reader goroutine or an arbitrary teardown delay.
func awaitCompletion(ctx context.Context, input *os.File) error {
	if input == nil || input.Fd() > math.MaxInt32 {
		return errCompletion
	}
	interrupts := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		fds := []unix.PollFd{{Fd: int32(input.Fd()), Events: unix.POLLIN | unix.POLLHUP}}
		_, err := unix.Poll(fds, 10)
		if err == unix.EINTR {
			interrupts++
			if interrupts == 3 {
				return errCompletion
			}
			continue
		}
		if err != nil || fds[0].Revents&(unix.POLLERR|unix.POLLNVAL) != 0 {
			return errCompletion
		}
		interrupts = 0
		if fds[0].Revents&(unix.POLLIN|unix.POLLHUP) == 0 {
			continue
		}
		var data [1]byte
		n, err := unix.Read(int(input.Fd()), data[:])
		if err != nil || n != 0 {
			return errCompletion
		}
		return ctx.Err()
	}
}
