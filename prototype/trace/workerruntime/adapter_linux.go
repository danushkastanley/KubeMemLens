package workerruntime

import (
	"context"
	"os"
	"os/exec"
	"sync/atomic"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/prototype/trace/targetfs"
	"github.com/danushkastanley/kube-memlens/prototype/trace/workeripc"
	"github.com/danushkastanley/kube-memlens/prototype/trace/workerprocess"
)

type adapter struct {
	owner    *Runtime
	spec     trace.Specification
	target   targetfs.Handle
	manifest string
	consumed atomic.Bool
}

func (a *adapter) Run(parent context.Context, spec trace.Specification, output trace.Output) (trace.Result, error) {
	failed := trace.Result{Version: trace.ContractVersion, Termination: trace.EngineFailed, Incomplete: true}
	if !a.consumed.CompareAndSwap(false, true) || parent.Err() != nil || spec != a.spec || output == nil || a.target.Target() != spec.Target() {
		return failed, ErrRuntime
	}
	if a.owner.enter() != nil {
		return failed, ErrRuntime
	}
	defer func() {
		if failure := recover(); failure != nil {
			a.owner.quarantine()
			panic(failure)
		}
		a.owner.leave()
	}()
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(a.owner.ctx, cancel)
	defer func() { stop(); cancel() }()
	a.owner.mu.Lock()
	owned, err := a.owner.snapshot()
	a.owner.mu.Unlock()
	if err != nil {
		return failed, ErrRuntime
	}
	defer func() {
		if owned.close() != nil {
			panic(workerprocess.ErrCleanupUnconfirmed)
		}
	}()
	owned.target, err = a.owner.exportTarget(ctx, a.target)
	if err != nil || ctx.Err() != nil {
		return failed, ErrRuntime
	}
	issued := time.Now().UTC()
	deadline := issued.Add(spec.Bounds().Duration)
	if end, ok := parent.Deadline(); ok && end.Before(deadline) {
		deadline = end.UTC()
	}
	request := workeripc.Request{Specification: spec, IssuedAt: issued, Deadline: deadline, ManifestSHA256: a.manifest}
	cmd := exec.Command("/proc/self/fd/4")
	cmd.Args = []string{"memlens-filecache-worker"}
	// Do not inherit loader, proxy, credential or diagnostic environment values.
	cmd.Env = []string{"GOTRACEBACK=none"}
	cmd.ExtraFiles = []*os.File{owned.target, owned.image, owned.bundle, owned.policy}
	return workerprocess.Run(ctx, cmd, request, output)
}
