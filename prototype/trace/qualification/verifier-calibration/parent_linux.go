package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	verifier "github.com/danushkastanley/kube-memlens/prototype/trace/qualification/verifier"
	fixture "github.com/danushkastanley/kube-memlens/prototype/trace/qualification/verifier-fixture"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func stamp() uint64 {
	var t unix.Timespec
	if unix.ClockGettime(unix.CLOCK_MONOTONIC, &t) != nil {
		panic("clock failed")
	}
	return uint64(t.Nano())
}
func group(path string, inode uint64) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "owned calibration group")
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Ino != inode {
		f.Close()
		return nil, errors.New("cgroup inode changed")
	}
	for name, want := range map[string]string{"memory.max": "67108864", "memory.swap.max": "0", "pids.max": "16", "cpu.max": "100000 100000", "cgroup.procs": ""} {
		raw, err := os.ReadFile(path + "/" + name)
		if err != nil || strings.TrimSpace(string(raw)) != want {
			f.Close()
			return nil, errors.New("cgroup bounds changed")
		}
	}
	return f, nil
}

type worker struct {
	cmd                 *exec.Cmd
	input               io.WriteCloser
	output              *bufio.Scanner
	waited, inputClosed bool
}

func launch(f *os.File, path string) (*worker, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(executable, "--fixture-child", "--acknowledge-fixed-verifier-calibration")
	cmd.Env = append(os.Environ(), "GOMAXPROCS=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{UseCgroupFD: true, CgroupFD: int(f.Fd()), Pdeathsig: syscall.SIGKILL}
	cmd.Stderr = os.Stderr
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		input.Close()
		output.Close()
		return nil, err
	}
	w := &worker{cmd: cmd, input: input, output: bufio.NewScanner(output)}
	w.output.Buffer(make([]byte, 1024), 4096)
	if !w.output.Scan() || w.output.Text() != "ready" {
		return nil, errors.Join(errors.New("fixture not ready"), w.abort())
	}
	raw, err := os.ReadFile(path + "/cgroup.procs")
	if err != nil || strings.TrimSpace(string(raw)) != strconv.Itoa(cmd.Process.Pid) {
		return nil, errors.Join(errors.New("fixture cgroup membership differs"), w.abort())
	}
	return w, nil
}
func (w *worker) abort() error {
	var result error
	if !w.waited {
		if err := w.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			result = errors.Join(result, err)
		}
		err := w.cmd.Wait()
		var exited *exec.ExitError
		if err != nil && !errors.As(err, &exited) {
			result = errors.Join(result, err)
		}
		w.waited = true
	}
	if !w.inputClosed {
		result = errors.Join(result, w.input.Close())
		w.inputClosed = true
	}
	return result
}
func (w *worker) finish() error {
	if _, err := fmt.Fprintln(w.input, "finish"); err != nil {
		return err
	}
	if err := w.input.Close(); err != nil {
		return err
	}
	w.inputClosed = true
	err := w.cmd.Wait()
	w.waited = true
	return err
}

type caseReport struct {
	Group         string          `json:"group"`
	CPU           uint32          `json:"cpu"`
	Fixture       fixture.Outcome `json:"fixture"`
	Before, After verifier.Totals
	SampleCount   int `json:"sampleCount"`
}

func parent(c config) (result error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	plan, err := verifier.NewProbePlan(c.Owner)
	if err != nil {
		return err
	}
	prefix := "/sys/fs/cgroup/kml-vcal-" + c.Owner
	if c.SelectedGroup != prefix+"-selected" || c.OtherGroup != prefix+"-excluded" || len(c.CPUs) < 2 {
		return errors.New("unowned fixture scope")
	}
	if err := sameKernel(c); err != nil {
		return err
	}
	selected, err := group(c.SelectedGroup, c.SelectedInode)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, selected.Close()) }()
	excluded, err := group(c.OtherGroup, c.OtherInode)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, excluded.Close()) }()
	probes, err := verifier.RegisterProbeSet(plan, c.BTF)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, probes.Close()) }()
	capture, err := verifier.OpenCapture(probes, selected, c.SelectedInode, c.CPUs)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, capture.Close()) }()
	merge, err := verifier.NewMerge(c.CPUs)
	if err != nil {
		return err
	}
	seen := 0
	wantedCPU := c.CPUs[0]
	consume := func(frame verifier.Frame) error {
		seen++
		if frame.CPU != wantedCPU {
			return errors.New("unexpected fixture CPU")
		}
		return merge.Push(frame)
	}
	settle := func() (verifier.Totals, error) {
		end := stamp() + 150_000_000
		for stamp() < end {
			if err := capture.Drain(consume); err != nil {
				return verifier.Totals{}, err
			}
			time.Sleep(5 * time.Millisecond)
		}
		return merge.Snapshot(stamp() - 100_000_000)
	}
	before, err := settle()
	if err != nil || before.CompletedCalls != 0 {
		return errors.New("initial owned capture not empty")
	}
	reports := []caseReport{}
	ids := []uint32{}
	stage := "start"
	defer func() {
		if result != nil {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"schemaVersion": 1, "partial": true, "stage": stage, "cases": reports, "samples": seen})
		}
	}()
	for _, scope := range []struct {
		name, path string
		file       *os.File
	}{{"selected", c.SelectedGroup, selected}, {"excluded", c.OtherGroup, excluded}} {
		child, err := launch(scope.file, scope.path)
		if err != nil {
			return err
		}
		defer func() { result = errors.Join(result, child.abort()) }()
		for index, which := range []fixture.Case{fixture.NoLog, fixture.WithLog, fixture.Rejected} {
			if err := sameKernel(c); err != nil {
				return err
			}
			stage = scope.name + "/" + string(which)
			cpu := c.CPUs[0]
			if index == 1 {
				cpu = c.CPUs[len(c.CPUs)-1]
			}
			wantedCPU = cpu
			startSeen := seen
			if _, err := fmt.Fprintf(child.input, "%s %d\n", which, cpu); err != nil {
				return err
			}
			if !child.output.Scan() {
				return errors.New("fixture result incomplete")
			}
			var out fixture.Outcome
			if json.Unmarshal(child.output.Bytes(), &out) != nil || out.Case != which {
				return errors.New("fixture result invalid")
			}
			after, err := settle()
			if err != nil {
				return err
			}
			reports = append(reports, caseReport{scope.name, cpu, out, before, after, seen - startSeen})
			if scope.name == "selected" {
				if after.CompletedCalls-before.CompletedCalls != 1 || after.FinalizedLogBytes-before.FinalizedLogBytes != uint64(out.RequiredLogBytes) || after.LogFailures != before.LogFailures || seen-startSeen != 3 {
					return errors.New("verifier call or log observation differs")
				}
				duration := after.DurationNanos - before.DurationNanos
				if duration == 0 || duration > out.SyscallUpperNanos {
					return errors.New("verifier duration outside syscall bound")
				}
				rejects := uint64(0)
				if !out.Accepted {
					rejects = 1
				}
				if after.RejectedCalls-before.RejectedCalls != rejects {
					return errors.New("verifier rejection differs")
				}
			} else if after != before || seen != startSeen {
				return errors.New("excluded cgroup entered observations")
			}
			before = after
			if out.ProgrammeID != 0 {
				ids = append(ids, out.ProgrammeID)
			}
		}
		if err := child.finish(); err != nil {
			return err
		}
	}
	counters, err := capture.Stop(consume)
	if err != nil {
		return err
	}
	total, err := merge.Finish(stamp())
	if err != nil {
		return err
	}
	if total.CompletedCalls != 3 || total.RejectedCalls != 1 || counters.Events != 9 || total.PendingCalls != 0 {
		return errors.New("final coverage differs")
	}
	if err := capture.Close(); err != nil {
		return err
	}
	counts, err := probes.Counts()
	if err != nil {
		return err
	}
	for _, value := range counts {
		if value.Missed != 0 {
			return errors.New("probe missed a return")
		}
	}
	if err := probes.Close(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := fixture.Absent(id); err != nil {
			return err
		}
	}
	if err := sameKernel(c); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"schemaVersion": 1, "cases": reports, "totals": total, "perf": counters, "probeCounts": counts, "ownedProgrammesAbsent": true, "probesRemoved": true})
}
