//go:build linux && (amd64 || arm64)

// memlens-filecache-worker runs only as the private supervised child. It accepts
// no CLI choices, gadget reference, arbitrary bytecode or installation paths.
package main

import (
	"context"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
	"github.com/danushkastanley/kube-memlens/prototype/trace/worker/sdk"
	"github.com/danushkastanley/kube-memlens/prototype/trace/workercontainment"
	"github.com/danushkastanley/kube-memlens/prototype/trace/workerinstall"
	"github.com/danushkastanley/kube-memlens/prototype/trace/workeripc"
)

func main() { os.Exit(run()) }

func run() (code int) {
	code = 2
	if len(os.Args) != 1 || workercontainment.Restrict() != nil || workercontainment.RequirePrivileges() != nil {
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	target, image, bundle, acceptance := os.NewFile(3, "target"), os.NewFile(4, "image"), os.NewFile(5, "bundle"), os.NewFile(6, "acceptance")
	transferred := false
	defer func() {
		for _, file := range []*os.File{image, bundle, acceptance} {
			if file == nil || file.Close() != nil {
				code = 2
			}
		}
		if !transferred && (target == nil || target.Close() != nil) {
			code = 2
		}
	}()
	policy, err := workerinstall.ReadDescriptor(acceptance)
	if err != nil || policy.VerifyRunning(image) != nil || !isDirectory(bundle) || !isPipe(os.Stdin) || !isPipe(os.Stdout) {
		return
	}
	root, err := os.OpenRoot("/proc/self/fd/5")
	if err != nil {
		return
	}
	defer func() {
		if root.Close() != nil {
			code = 2
		}
	}()
	input, err := pollablePipe(os.Stdin)
	if err != nil {
		return
	}
	defer func() {
		if input.Close() != nil {
			code = 2
		}
	}()
	output, err := pollablePipe(os.Stdout)
	if err != nil {
		return
	}
	defer func() {
		if output.Close() != nil {
			code = 2
		}
	}()
	if input.SetReadDeadline(time.Now().Add(time.Second)) != nil {
		return
	}
	request, err := workeripc.ReadRequest(input)
	if err != nil || !request.Deadline.After(time.Now()) || request.IssuedAt.After(time.Now().Add(5*time.Millisecond)) {
		return
	}
	signalLifetime := ctx
	ctx, cancel := context.WithDeadline(ctx, request.Deadline)
	defer cancel()
	id := filecache.ArtifactID{Kind: request.Specification.Kind(), Architecture: runtime.GOARCH}
	digest, err := policy.ManifestSHA256(id)
	if err != nil || digest != request.ManifestSHA256 {
		return
	}
	programme, err := policy.Programme(root, id)
	if err != nil || ctx.Err() != nil {
		return
	}
	writer, err := workeripc.NewWriter(pipeWriter{output}, request)
	if err != nil {
		return
	}
	adapter, err := sdk.NewAdapter(signalLifetime, programme, request.Specification, target, writer.Ready)
	if err != nil {
		return
	}
	transferred = true
	result, runErr := adapter.Run(ctx, request.Specification, writer)
	if runErr != nil {
		result.Termination = trace.EngineFailed
		result.Counts = trace.Counts{}
		result.Incomplete = true
	}
	if writer.Finish(result) != nil || runErr != nil {
		return
	}
	return 0
}

type pipeWriter struct{ file *os.File }

func (w pipeWriter) Write(data []byte) (int, error) {
	// Also applies to the terminal reserve after observation expiry. Parent
	// supervision independently bounds the worker's overall lifetime and reap.
	if w.file.SetWriteDeadline(time.Now().Add(time.Second)) != nil {
		return 0, workeripc.ErrOutput
	}
	return w.file.Write(data)
}
func isDirectory(file *os.File) bool {
	if file == nil {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.IsDir()
}
func isPipe(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeNamedPipe != 0
}
