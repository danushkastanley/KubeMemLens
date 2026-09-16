package main

import (
	"encoding/binary"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// PTRACE_GET_SYSCALL_INFO has a fixed UAPI prefix and union layout. Only the
// syscall number, BPF command and return value are examined; addresses, payloads,
// registers and instructions are neither retained nor modified.
func linkCall(tid int, task *tracedTask, result *partialResult) (bool, error) {
	var data [88]byte
	size, _, errno := unix.Syscall6(unix.SYS_PTRACE, unix.PTRACE_GET_SYSCALL_INFO, uintptr(tid), uintptr(len(data)), uintptr(unsafe.Pointer(&data[0])), 0, 0)
	if errno != 0 || size < 24 {
		return false, traceFault("syscall_info")
	}
	switch data[0] {
	case 1:
		if size < 80 {
			return false, traceFault("syscall_info")
		}
		isBPF := binary.LittleEndian.Uint64(data[24:32]) == unix.SYS_BPF
		if isBPF {
			result.BPFCalls++
		}
		task.linkEntry = isBPF && binary.LittleEndian.Uint64(data[32:40]) == unix.BPF_LINK_CREATE
		if task.linkEntry {
			result.LinkEntries++
		}
		return false, nil
	case 2:
		if size < 33 {
			return false, traceFault("syscall_info")
		}
		created := task.linkEntry && data[32] == 0 && int64(binary.LittleEndian.Uint64(data[24:32])) >= 0
		task.linkEntry = false
		return created, nil
	default:
		return false, traceFault("syscall_info")
	}
}

func syscallPartial(parent *process, workers []*process, targets map[uint64]bool, links int) (result partialResult, resultErr error) {
	// Linux ptrace ownership belongs to the tracer task, not an arbitrary Go
	// runtime thread. EXITKILL and the unchanged node watchdog bound helper failure.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	tracer := &taskTracer{workers: workers, tasks: map[int]*tracedTask{}}
	defer func() {
		if tracer.finish() != nil {
			resultErr = errOwnership
		}
	}()
	if sameChildren(parent, workers, targets) != nil {
		return result, traceFault("initial_ownership")
	}
	if err := tracer.stopAll(); err != nil {
		return result, err
	}
	for tid := range tracer.tasks {
		if tracer.resume(tid, 0) != nil {
			return result, errOwnership
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for result.Observations < 20000 && time.Now().Before(deadline) {
		tid, status, err := tracer.wait()
		if err != nil {
			return result, err
		}
		if tid == 0 {
			continue
		}
		if status.Exited() || status.Signaled() {
			return result, errOwnership
		}
		task := tracer.tasks[tid]
		result.Observations++
		signal := 0
		if status.StopSignal() == unix.SIGTRAP|0x80 {
			created, err := linkCall(tid, task, &result)
			if err != nil {
				return result, err
			}
			if created {
				action, clockErr := readClock()
				if clockErr != nil {
					return result, traceFault("clock")
				}
				result.Action = &action
				if err := tracer.stopAll(); err != nil {
					return result, err
				}
				if sameChildren(parent, workers, targets) != nil {
					return result, traceFault("partial_ownership")
				}
				partial := false
				for _, worker := range workers {
					value, err := inspectWorkers([]*process{worker}, 0)
					if err != nil {
						return result, err
					}
					result.WorkersBefore = append(result.WorkersBefore, value)
					count := len(value.Objects["link"])
					partial = partial || (value.ActiveControls == 0 && count > 0 && count < links)
				}
				if !partial {
					return result, traceFault("partial_state_not_observed")
				}
				before, inspectErr := inspectWorkers(workers, 0)
				if inspectErr != nil {
					return result, inspectErr
				}
				result.Before = &before
				result.Found, result.Signalled = true, len(workers)
				return result, nil
			}
		} else if status.TrapCause() == 0 && status.StopSignal() != unix.SIGSTOP {
			signal = int(status.StopSignal())
		}
		if tracer.resume(tid, signal) != nil {
			return result, errOwnership
		}
	}
	return result, traceFault("syscall_observation_bound")
}

func interruptAtSyscall(parent *process, digest string, targets map[uint64]bool, links int, ready func() error) (result partialResult, resultErr error) {
	if (runtime.GOARCH != "arm64" && runtime.GOARCH != "amd64") || (links != 2 && links != 5) || ready == nil {
		return result, errOwnership
	}
	image, err := retainedImage(parent, digest)
	if err != nil {
		return result, err
	}
	defer func() {
		if image.file.Close() != nil {
			resultErr = errOwnership
		}
	}()
	if ready() != nil {
		return result, errOwnership
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		workers, excluded, err := ownedChildrenWithOpen(parent, targets, func(pid int) (*process, error) { return openSealedProcess(pid, image) })
		if err != nil {
			return result, err
		}
		if excluded == 0 && len(workers) == len(targets) {
			result, resultErr = syscallPartial(parent, workers, targets, links)
			if closeProcesses(workers) != nil {
				resultErr = errOwnership
			}
			return result, resultErr
		}
		if closeProcesses(workers) != nil {
			return result, errOwnership
		}
		time.Sleep(time.Millisecond)
	}
	return result, errOwnership
}
