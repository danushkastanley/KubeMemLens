package main

import (
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"time"
)

type config struct {
	Owner, BTF, Boot, SelectedGroup, OtherGroup string
	SelectedInode, OtherInode                   uint64
	CPUs                                        []uint32
}

func main() {
	if unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{}) != nil {
		os.Exit(2)
	}
	if len(os.Args) == 3 && os.Args[1] == "--fixture-child" && os.Args[2] == "--acknowledge-fixed-verifier-calibration" {
		if err := child(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) != 2 || os.Args[1] != "--acknowledge-fixed-verifier-calibration" {
		os.Exit(2)
	}
	timer := time.AfterFunc(15*time.Second, func() { os.Exit(124) })
	defer timer.Stop()
	c, err := readConfiguration(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration failed")
		os.Exit(2)
	}
	if err := parent(c); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
