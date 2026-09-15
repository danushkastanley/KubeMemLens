package sdk

import (
	"context"
	"fmt"
	"github.com/cilium/ebpf"
	"runtime"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
	gadgetcontext "github.com/inspektor-gadget/inspektor-gadget/pkg/gadget-context"
	"github.com/inspektor-gadget/inspektor-gadget/pkg/gadget-service/api"
	"github.com/inspektor-gadget/inspektor-gadget/pkg/logger"
	"github.com/inspektor-gadget/inspektor-gadget/pkg/operators"
	ebpfoperator "github.com/inspektor-gadget/inspektor-gadget/pkg/operators/ebpf"
	"github.com/spf13/viper"
)

type controlledContext struct {
	*gadgetcontext.GadgetContext
	validate func(context.Context, map[string]*ebpf.Map) error
}

func (c *controlledContext) BeforeAttach(owned map[string]*ebpf.Map) error {
	return c.validate(c.Context(), owned)
}

// prepare chooses only the fixed eBPF image operator. It must run in the bounded
// worker after programme acceptance: SDK analysis may perform feature probes.
// It deliberately avoids generic runtime.Init, OCI fetching and data operators.
func prepare(ctx context.Context, p *filecache.Programme, spec trace.Specification, deadline uint64, log logger.Logger, validate func(context.Context, map[string]*ebpf.Map) error) (*controlledContext, operators.ImageOperatorInstance, error) {
	if ebpfoperator.ConstrainedWorkerPolicyVersion != 1 || validate == nil || p == nil || spec.Validate() != nil || p.Manifest().Kind != spec.Kind() || p.Manifest().Architecture != runtime.GOARCH || deadline == 0 || log == nil || ctx.Err() != nil {
		return nil, nil, ErrWorker
	}
	store, err := newStore(p)
	if err != nil {
		return nil, nil, err
	}
	gctx := &controlledContext{gadgetcontext.New(ctx, store.descriptor.Digest.String(), gadgetcontext.WithLogger(log), gadgetcontext.WithTimeout(spec.Bounds().Duration), gadgetcontext.WithOrasReadonlyTarget(store)), validate}
	// The fixed object needs no user annotations, exporters or config file.
	gctx.SetVar("config", viper.New())
	op, ok := operators.GetImageOperatorForMediaType(objectMediaType)
	if !ok || op.Name() != "ebpf" {
		gctx.Cancel()
		return nil, nil, ErrWorker
	}
	parameters := api.ParamValues{"target_cgroup": fmt.Sprint(spec.Target().CgroupID), "deadline_ns": fmt.Sprint(deadline), "event_limit": fmt.Sprint(spec.Bounds().Events), "trace-pipe": "false"}
	if spec.Kind() == trace.Files {
		parameters["path_bytes"] = "0"
		if spec.Paths() == trace.ConfirmedPaths {
			parameters["path_bytes"] = fmt.Sprint(spec.Bounds().PathBytes)
		}
	}
	instance, err := op.InstantiateImageOperator(gctx, store, store.descriptor, parameters)
	if err != nil || instance == nil {
		gctx.Cancel()
		return nil, nil, ErrWorker
	}
	if len(gctx.GetDataSources()) != 0 {
		_ = instance.Close(gctx)
		gctx.Cancel()
		return nil, nil, ErrWorker
	}
	if err := ctx.Err(); err != nil {
		_ = instance.Close(gctx)
		gctx.Cancel()
		return nil, nil, ErrWorker
	}
	return gctx, instance, nil
}

// Keep the worker preparation deadline independent from an admitted 5-minute
// observation bound; supervision must kill and reap an unresponsive child.
const preparationTimeout = 5 * time.Second
