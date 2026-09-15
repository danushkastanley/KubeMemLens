// Package workerruntime connects installation-owned worker artefacts to the
// optional node service. It imports no SDK and loads no BPF programme itself.
package workerruntime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
	"github.com/danushkastanley/kube-memlens/prototype/trace/nodebinding"
	"github.com/danushkastanley/kube-memlens/prototype/trace/targetfs"
	"github.com/danushkastanley/kube-memlens/prototype/trace/workerinstall"
	"golang.org/x/sys/unix"
)

var ErrRuntime = errors.New("accepted trace worker runtime unavailable")

type Runtime struct {
	mu            sync.Mutex
	configuration *workerinstall.Policy
	owned         resources
	ctx           context.Context
	cancel        context.CancelFunc
	exportTarget  func(context.Context, targetfs.Handle) (*os.File, error)
	closed        bool
	poisoned      bool
	running       int
	idle          chan struct{}
	closeOnce     sync.Once
	closeErr      error
}

// New verifies installation artefacts once and retains their descriptors.
// Paths and acceptance come only from node installation, never admission input.
func New(ctx context.Context, policy *workerinstall.Policy, executable, bundle string) (result *Runtime, resultErr error) {
	if ctx == nil || ctx.Err() != nil || policy == nil || !filepath.IsAbs(executable) || !filepath.IsAbs(bundle) {
		return nil, ErrRuntime
	}
	r := &Runtime{configuration: policy, exportTarget: targetfs.ExportForWorker, idle: make(chan struct{})}
	close(r.idle)
	retained := false
	defer func() {
		if !retained {
			if r.owned.close() != nil {
				result = nil
				resultErr = ErrRuntime
			}
		}
	}()
	var err error
	r.owned.image, err = policy.Executable(executable, runtime.GOARCH)
	if err != nil {
		return nil, ErrRuntime
	}
	r.owned.policy, err = policy.Descriptor()
	if err != nil {
		return nil, ErrRuntime
	}
	r.owned.bundle, err = os.OpenFile(bundle, os.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, ErrRuntime
	}
	r.owned.root, err = os.OpenRoot("/proc/self/fd/" + strconv.FormatUint(uint64(r.owned.bundle.Fd()), 10))
	if err != nil {
		return nil, ErrRuntime
	}
	accepted := 0
	for _, kind := range []trace.Kind{trace.Files, trace.Cache} {
		id := filecache.ArtifactID{Kind: kind, Architecture: runtime.GOARCH}
		if _, err := policy.ManifestSHA256(id); err != nil {
			continue
		}
		if _, err := policy.Programme(r.owned.root, id); err != nil {
			return nil, ErrRuntime
		}
		accepted++
	}
	if accepted == 0 || ctx.Err() != nil {
		return nil, ErrRuntime
	}
	r.ctx, r.cancel = context.WithCancel(ctx)
	retained = true
	return r, nil
}

func (r *Runtime) Prepare(ctx context.Context, spec trace.Specification, handle targetfs.Handle) (nodebinding.Prepared, error) {
	if ctx.Err() != nil || spec.Validate() != nil || handle == nil || handle.Target() != spec.Target() || handle.Check(ctx) != nil {
		return nodebinding.Prepared{}, ErrRuntime
	}
	r.mu.Lock()
	if r.closed || r.poisoned || r.ctx.Err() != nil {
		r.mu.Unlock()
		return nodebinding.Prepared{}, ErrRuntime
	}
	root, err := r.owned.root.OpenRoot(".")
	r.mu.Unlock()
	if err != nil {
		return nodebinding.Prepared{}, ErrRuntime
	}
	id := filecache.ArtifactID{Kind: spec.Kind(), Architecture: runtime.GOARCH}
	_, verifyErr := r.configuration.Programme(root, id)
	closeErr := root.Close()
	if verifyErr != nil || closeErr != nil || ctx.Err() != nil || r.ctx.Err() != nil {
		return nodebinding.Prepared{}, ErrRuntime
	}
	digest, err := r.configuration.ManifestSHA256(id)
	if err != nil {
		return nodebinding.Prepared{}, ErrRuntime
	}
	engine, err := trace.NewEngine(&adapter{owner: r, spec: spec, target: handle, manifest: digest})
	if err != nil {
		return nodebinding.Prepared{}, ErrRuntime
	}
	return nodebinding.Prepared{StreamVersion: traceframe.AggregateVersion, Engine: engine, EngineDigest: r.configuration.EngineDigest(), ProgrammeDigest: r.configuration.ProgrammeDigest()}, nil
}

// Close prevents new work, cancels active workers and waits for confirmed exits.
// Unknown cleanup keeps the runtime poisoned and prevents a successful close.
func (r *Runtime) Close(ctx context.Context) error {
	r.closeOnce.Do(func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.closed = true
		r.cancel()
		r.closeErr = r.owned.close()
	})
	r.mu.Lock()
	idle := r.idle
	poisoned := r.poisoned
	r.mu.Unlock()
	if poisoned {
		return ErrRuntime
	}
	select {
	case <-idle:
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.poisoned || r.closeErr != nil {
			return ErrRuntime
		}
		return nil
	case <-ctx.Done():
		return ErrRuntime
	}
}

func (r *Runtime) enter() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.poisoned || r.ctx.Err() != nil || r.running >= 2 {
		return ErrRuntime
	}
	if r.running == 0 {
		r.idle = make(chan struct{})
	}
	r.running++
	return nil
}

func (r *Runtime) leave() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.running--
	if r.running == 0 {
		close(r.idle)
	}
}

func (r *Runtime) quarantine() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.poisoned = true
	r.cancel()
}
