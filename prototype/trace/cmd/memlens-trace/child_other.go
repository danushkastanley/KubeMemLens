//go:build !linux

package main

import "os/exec"

// Non-Linux workers only report the unsupported platform and cannot load BPF.
func configureChild(*exec.Cmd) {}
