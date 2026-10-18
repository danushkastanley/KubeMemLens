package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	fixture "github.com/danushkastanley/kube-memlens/prototype/trace/qualification/verifier-fixture"
	"golang.org/x/sys/unix"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

func child() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for resource, limit := range map[int]uint64{unix.RLIMIT_CORE: 0, unix.RLIMIT_CPU: 2, unix.RLIMIT_NOFILE: 64} {
		if unix.Setrlimit(resource, &unix.Rlimit{Cur: limit, Max: limit}) != nil {
			return errors.New("fixture limit failed")
		}
	}
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var caps [2]unix.CapUserData
	if unix.Capget(&header, &caps[0]) != nil {
		return errors.New("capability read failed")
	}
	const allowed = uint32(1<<(unix.CAP_BPF-32) | 1<<(unix.CAP_PERFMON-32))
	if caps[1].Permitted&allowed != allowed {
		return errors.New("fixture capabilities unavailable")
	}
	caps = [2]unix.CapUserData{{}, {Effective: allowed, Permitted: allowed}}
	_, _, errno := syscall.AllThreadsSyscall(unix.SYS_CAPSET, uintptr(unsafe.Pointer(&header)), uintptr(unsafe.Pointer(&caps[0])), 0)
	runtime.KeepAlive(&header)
	runtime.KeepAlive(&caps)
	if errno != 0 {
		return errors.New("fixture capability restriction failed")
	}
	_, _, errno = syscall.AllThreadsSyscall6(unix.SYS_PRCTL, unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0, 0)
	if errno != 0 {
		return errors.New("fixture no-new-privileges failed")
	}
	if unix.Capget(&header, &caps[0]) != nil || caps[0] != (unix.CapUserData{}) || caps[1] != (unix.CapUserData{Effective: allowed, Permitted: allowed}) {
		return errors.New("fixture capabilities changed")
	}
	timer := time.AfterFunc(8*time.Second, func() { os.Exit(124) })
	defer timer.Stop()
	fmt.Println("ready")
	scan := bufio.NewScanner(os.Stdin)
	scan.Buffer(make([]byte, 64), 64)
	count := 0
	for scan.Scan() {
		if scan.Text() == "finish" {
			return nil
		}
		count++
		if count > 3 {
			return errors.New("fixture load count exceeded")
		}
		fields := strings.Fields(scan.Text())
		if len(fields) != 2 {
			return errors.New("fixture command invalid")
		}
		cpu, err := strconv.Atoi(fields[1])
		if err != nil || cpu < 0 || cpu >= 1024 {
			return errors.New("fixture CPU invalid")
		}
		var affinity unix.CPUSet
		affinity.Set(cpu)
		if unix.SchedSetaffinity(0, &affinity) != nil {
			return errors.New("fixture affinity failed")
		}
		out, err := fixture.Run(fixture.Case(fields[0]))
		if encodeErr := json.NewEncoder(os.Stdout).Encode(out); encodeErr != nil {
			return encodeErr
		}
		if err != nil {
			return err
		}
	}
	return errors.New("fixture input incomplete")
}
