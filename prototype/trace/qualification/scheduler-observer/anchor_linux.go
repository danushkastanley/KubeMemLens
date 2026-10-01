package main

import (
	"errors"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

var errAnchor = errors.New("scheduler owner process changed or exited")

type anchorConfiguration struct {
	PID   int    `json:"pid"`
	Start uint64 `json:"start"`
}

type processAnchor struct{ fd int }

func processStart(pid int) (uint64, error) {
	raw, err := boundedRead("/proc/"+strconv.Itoa(pid)+"/stat", 4096, false)
	if err != nil {
		return 0, errAnchor
	}
	// The comm field can contain spaces and parentheses. Only the numeric start
	// field is retained; neither the raw stat record nor its name is evidence.
	end := strings.LastIndex(string(raw), ") ")
	if end < 0 {
		return 0, errAnchor
	}
	fields := strings.Fields(string(raw[end+2:]))
	if len(fields) < 20 {
		return 0, errAnchor
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || start == 0 {
		return 0, errAnchor
	}
	return start, nil
}

func bindAnchor(cfg anchorConfiguration) (*processAnchor, error) {
	if cfg.PID <= 1 || cfg.PID > 0x7fffffff || cfg.Start == 0 {
		return nil, errAnchor
	}
	fd, err := unix.PidfdOpen(cfg.PID, 0)
	if err != nil {
		return nil, errAnchor
	}
	anchor := &processAnchor{fd: fd}
	start, err := processStart(cfg.PID)
	if err != nil || start != cfg.Start || anchor.alive() != nil {
		return nil, errors.Join(errAnchor, anchor.close())
	}
	return anchor, nil
}

func (a *processAnchor) alive() error {
	fds := []unix.PollFd{{Fd: int32(a.fd), Events: unix.POLLIN}}
	count, err := unix.Poll(fds, 0)
	if err != nil || count != 0 || fds[0].Revents != 0 {
		return errAnchor
	}
	return nil
}

func (a *processAnchor) close() error {
	if unix.Close(a.fd) != nil {
		return errAnchor
	}
	return nil
}
