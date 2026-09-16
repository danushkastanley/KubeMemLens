package main

import (
	"errors"
	"os"
	"strconv"
	"time"

	"golang.org/x/sys/unix"
)

type traceFault string

func (e traceFault) Error() string { return string(e) }

type tracedTask struct {
	owner                          int
	stopped, configured, linkEntry bool
}
type taskTracer struct {
	workers []*process
	tasks   map[int]*tracedTask
}

const traceOptions = unix.PTRACE_O_TRACESYSGOOD | unix.PTRACE_O_TRACECLONE | unix.PTRACE_O_EXITKILL

func (t *taskTracer) discover() (bool, error) {
	added := false
	for owner, worker := range t.workers {
		if worker.check() != nil {
			return false, errOwnership
		}
		entries, err := os.ReadDir(procPath(worker.pid, "task"))
		if err != nil || len(entries) > 256 {
			return false, errOwnership
		}
		for _, entry := range entries {
			tid, err := strconv.Atoi(entry.Name())
			if err != nil || tid <= 1 {
				return false, errOwnership
			}
			if t.tasks[tid] != nil {
				continue
			}
			if len(t.tasks) >= 512 {
				return false, errOwnership
			}
			if err := unix.PtraceSeize(tid); err != nil {
				existing, checkErr := t.ownedAutoThread(tid)
				if checkErr != nil || existing.owner != owner {
					return false, traceFault("seize_or_clone_identity")
				}
			} else {
				t.tasks[tid] = &tracedTask{owner: owner}
			}
			added = true
			if unix.PtraceInterrupt(tid) != nil {
				return false, traceFault("initial_interrupt")
			}
		}
	}
	return added, nil
}

func (t *taskTracer) wait() (int, unix.WaitStatus, error) {
	var status unix.WaitStatus
	tid, err := unix.Wait4(-1, &status, unix.WNOHANG|unix.WALL, nil)
	if errors.Is(err, unix.EINTR) {
		return 0, 0, nil
	}
	if errors.Is(err, unix.ECHILD) && len(t.tasks) == 0 {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, traceFault("wait")
	}
	if tid == 0 {
		return 0, 0, nil
	}
	task := t.tasks[tid]
	if task == nil {
		var err error
		task, err = t.ownedAutoThread(tid)
		if err != nil {
			return 0, 0, err
		}
	}
	if status.Exited() || status.Signaled() {
		delete(t.tasks, tid)
		return tid, status, nil
	}
	if !status.Stopped() {
		return 0, 0, errOwnership
	}
	task.stopped = true
	if !task.configured {
		data, err := readBounded(procPath(tid, "status"), 16384)
		if err != nil || field(data, "Tgid") != strconv.Itoa(t.workers[task.owner].pid) || unix.PtraceSetOptions(tid, traceOptions) != nil {
			return 0, 0, traceFault("thread_identity_or_options")
		}
		task.configured = true
	}
	if status.TrapCause() == unix.PTRACE_EVENT_CLONE {
		child, err := unix.PtraceGetEventMsg(tid)
		if err != nil || child <= 1 || child > 1<<31-1 || len(t.tasks) >= 512 {
			return 0, 0, errOwnership
		}
		if existing := t.tasks[int(child)]; existing != nil {
			if existing.owner != task.owner {
				return 0, 0, errOwnership
			}
		} else {
			t.tasks[int(child)] = &tracedTask{owner: task.owner}
		}
	}
	return tid, status, nil
}

func (t *taskTracer) stopAll() error {
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, err := t.discover(); err != nil {
			return err
		}
		all := len(t.tasks) > 0
		for tid, task := range t.tasks {
			if task.stopped {
				continue
			}
			all = false
			if err := unix.PtraceInterrupt(tid); err != nil && !errors.Is(err, unix.ESRCH) {
				return errOwnership
			}
		}
		if all {
			return nil
		}
		if _, _, err := t.wait(); err != nil {
			return err
		}
		time.Sleep(100 * time.Microsecond)
	}
	return traceFault("stop_deadline")
}

func (t *taskTracer) resume(tid int, signal int) error {
	task := t.tasks[tid]
	if task == nil || !task.stopped || !task.configured || unix.PtraceSyscall(tid, signal) != nil {
		return traceFault("resume")
	}
	task.stopped = false
	return nil
}

// Always terminate only the originally verified processes and drain their
// ptrace exit notifications so the real node supervisor can reap its children.
func (t *taskTracer) finish() error {
	for _, worker := range t.workers {
		if err := unix.PidfdSendSignal(worker.fd, unix.SIGKILL, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
			return errOwnership
		}
	}
	deadline := time.Now().Add(time.Second)
	for len(t.tasks) > 0 && time.Now().Before(deadline) {
		tid, status, err := t.wait()
		if err != nil {
			return err
		}
		if tid != 0 && status.Stopped() {
			if unix.PtraceCont(tid, int(unix.SIGKILL)) != nil {
				return errOwnership
			}
			t.tasks[tid].stopped = false
		}
		if tid == 0 {
			time.Sleep(100 * time.Microsecond)
		}
	}
	if len(t.tasks) != 0 {
		return traceFault("exit_deadline")
	}
	return nil
}

// Clone notifications and the child's initial stop can be delivered in either
// order. Recognise only a task already traced by this exact OS thread and in an
// originally verified worker's thread group; never adopt another debugger's task.
func (t *taskTracer) ownedAutoThread(tid int) (*tracedTask, error) {
	if tid <= 1 || len(t.tasks) >= 512 {
		return nil, errOwnership
	}
	data, err := readBounded(procPath(tid, "status"), 16384)
	if err != nil || field(data, "TracerPid") != strconv.Itoa(unix.Gettid()) {
		return nil, errOwnership
	}
	for owner, worker := range t.workers {
		if field(data, "Tgid") == strconv.Itoa(worker.pid) && worker.check() == nil {
			task := &tracedTask{owner: owner}
			t.tasks[tid] = task
			return task, nil
		}
	}
	return nil, errOwnership
}
