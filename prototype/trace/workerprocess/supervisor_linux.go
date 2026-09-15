// Package workerprocess supervises one installation-selected Linux worker.
// It contains no BPF loader or programme selection from user requests.
package workerprocess

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/prototype/trace/workeripc"
)

var ErrWorker = errors.New("supervised trace worker failed")
var ErrCleanupUnconfirmed = errors.New("trace worker cleanup unconfirmed")

const startupTimeout = 5 * time.Second
const signalGrace = 500 * time.Millisecond
const exitTimeout = 2 * time.Second
const outputTimeout = time.Second

type received struct {
	result trace.Result
	err    error
}

type stopMode uint8

const (
	waitForExit stopMode = iota
	interruptWorker
)

// Run owns process lifecycle and private pipes. cmd must select the verified
// installation executable and inherited descriptors; no caller-controlled
// command line is permitted. Its first ExtraFile is the retained target cgroup.
// The caller retains its own cgroup handle throughout Run.
//
// If SIGKILL cannot be followed by confirmed reaping within the bound, Run
// panics with ErrCleanupUnconfirmed. The node execution boundary must quarantine
// that execution and retain its lease, as it does for any adapter panic. Returning
// an ordinary engine error would incorrectly authorise releasing that handle.
func Run(parent context.Context, cmd *exec.Cmd, request workeripc.Request, output trace.Output) (trace.Result, error) {
	failed := trace.Result{Version: trace.ContractVersion, Termination: trace.EngineFailed, Incomplete: true}
	if parent.Err() != nil || request.Validate() != nil || !request.Deadline.After(time.Now()) ||
		request.IssuedAt.After(time.Now().Add(5*time.Millisecond)) || output == nil || cmd == nil || cmd.Process != nil ||
		cmd.Stdin != nil || cmd.Stdout != nil || cmd.Stderr != nil || cmd.Cancel != nil || cmd.SysProcAttr != nil || cmd.WaitDelay != 0 || len(cmd.ExtraFiles) == 0 || cmd.ExtraFiles[0] == nil {
		return failed, ErrWorker
	}
	ctx, cancel := context.WithDeadline(parent, request.Deadline)
	defer cancel()
	input, childInput, childOutput, outputPipe, err := pipes()
	if err != nil {
		return failed, ErrWorker
	}
	defer func() { _ = input.Close(); _ = childInput.Close(); _ = childOutput.Close(); _ = outputPipe.Close() }()
	cmd.Stdin, cmd.Stdout = childInput, childOutput
	// Nil Stderr goes directly to /dev/null. In particular, do not create a Go
	// copying goroutine that could retain arbitrary kernel/verifier diagnostics.
	// Supervision signals the owned process handle; it needs no process group.
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	// Linux ties Pdeathsig to the launching thread. Keep that thread alive for
	// the entire worker lifetime, even while this goroutine waits on channels.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	startup := time.NewTimer(startupTimeout)
	defer startup.Stop()
	if cmd.Start() != nil {
		return failed, ErrWorker
	}
	_ = childInput.Close()
	_ = childOutput.Close()
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	// From this point even a panic in protocol/output handling must reap the
	// process before unwinding. A timed-out reap deliberately remains a panic.
	finalised := false
	defer func() {
		if !finalised {
			finalised = true
			_ = terminate(cmd.Process, waited, interruptWorker)
		}
	}()
	if input.SetWriteDeadline(time.Now().Add(outputTimeout)) != nil || workeripc.WriteRequest(input, request) != nil || input.Close() != nil {
		finalised = true
		_ = terminate(cmd.Process, waited, interruptWorker)
		return failed, ErrWorker
	}
	ready := make(chan struct{}, 1)
	read := make(chan received, 1)
	eventDeadline, _ := ctx.Deadline()
	go func() {
		response := received{err: ErrWorker}
		defer func() {
			if recover() != nil {
				response = received{err: ErrWorker}
			}
			read <- response
		}()
		response.result, response.err = workeripc.ReadStream(outputPipe, request, activeOutput{ctx, eventDeadline, output}, func() { ready <- struct{}{} })
	}()
	var response received
	var stopped bool
	var readerFinished bool
	var naturalExpiry bool
	for !stopped {
		select {
		case <-ready:
			startup.Stop()
		case response = <-read:
			stopped = true
			readerFinished = true
		case <-startup.C:
			stopped = true
			response.err = ErrWorker
		case <-ctx.Done():
			stopped = true
			response.err = ErrWorker
			naturalExpiry = errors.Is(ctx.Err(), context.DeadlineExceeded) && !time.Now().Before(request.Deadline)
		}
	}
	// A terminal message is not authority to return before the process exits.
	cancel()
	finalised = true
	mode := waitForExit
	if response.err != nil && !naturalExpiry {
		mode = interruptWorker
	}
	exitErr := terminate(cmd.Process, waited, mode)
	if !naturalExpiry || exitErr != nil {
		_ = outputPipe.Close()
	}
	if !readerFinished {
		// Output implementations are required to honour their own one-second
		// write bound. A retained callback also prevents safe lease release.
		timer := time.NewTimer(outputTimeout)
		defer timer.Stop()
		select {
		case terminal := <-read:
			if naturalExpiry {
				response = terminal
			}
		case <-timer.C:
			panic(ErrCleanupUnconfirmed)
		}
	}
	_ = outputPipe.Close()
	if response.err != nil || exitErr != nil {
		return failed, ErrWorker
	}
	return response.result, nil
}

func pipes() (input, childInput, childOutput, output *os.File, err error) {
	childInput, input, err = os.Pipe()
	if err != nil {
		return
	}
	output, childOutput, err = os.Pipe()
	if err != nil {
		_ = childInput.Close()
		_ = input.Close()
	}
	return
}

// Signals use os.Process's owned process handle, not a numeric process/group
// ID that might be reused after Wait. The accepted worker profile must prevent
// creating descendant processes; this supervisor owns only the direct child.
func terminate(process *os.Process, waited <-chan error, mode stopMode) error {
	deadline := time.Now().Add(exitTimeout)
	select {
	case err := <-waited:
		return err
	default:
	}
	gracePeriod := workeripc.NormalExitGrace
	if mode == interruptWorker {
		gracePeriod = signalGrace
		_ = process.Signal(syscall.SIGTERM)
	}
	grace := time.NewTimer(gracePeriod)
	defer grace.Stop()
	select {
	case err := <-waited:
		return err
	case <-grace.C:
	}
	_ = process.Kill()
	return awaitReap(waited, time.Until(deadline))
}

func awaitReap(waited <-chan error, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-waited:
		return err
	case <-timer.C:
		panic(ErrCleanupUnconfirmed)
	}
}
