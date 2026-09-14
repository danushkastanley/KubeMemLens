package admissionapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
	"github.com/danushkastanley/kube-memlens/prototype/trace/nodebinding"
	"github.com/danushkastanley/kube-memlens/prototype/trace/streamhttp"
	"k8s.io/apiserver/pkg/authentication/user"
)

// StreamProxy has an installation-owned digest allowlist. Empty means no
// approved programme; it cannot be populated by request parameters.
type StreamProxy struct{ programmes map[trace.Kind]string }

func NewStreamProxy(programmes map[trace.Kind]string) (*StreamProxy, error) {
	p := &StreamProxy{programmes: map[trace.Kind]string{}}
	for kind, digest := range programmes {
		if (kind != trace.Files && kind != trace.Cache && kind != trace.OOM) || len(digest) != 71 || !strings.HasPrefix(digest, "sha256:") || strings.Trim(digest[7:], "0123456789abcdef") != "" {
			return nil, admission.ErrUnavailable
		}
		p.programmes[kind] = digest
	}
	return p, nil
}
func (p *StreamProxy) serve(parent context.Context, w http.ResponseWriter, lease *admission.Lease) error {
	ctx := parent
	a := lease.Admission()
	digest, approved := p.programmes[a.Specification().Kind()]
	if !approved {
		return admission.ErrUnavailable
	}
	binding, ok := lease.Binding().(nodebinding.StreamBinding)
	if !ok {
		return admission.ErrUnavailable
	}
	ctx, cancel := context.WithDeadline(ctx, lease.Deadline().Add(2*time.Second))
	defer cancel()
	source, err := binding.OpenStream(ctx, lease.Deadline())
	if err != nil {
		return err
	}
	defer source.Close()
	// Cancellation from permission loss/explicit DELETE interrupts blocked reads.
	// Normal expiry allows only bounded terminal drain; frame timestamps stay capped.
	stopped := context.AfterFunc(lease.Context(), func() {
		if !errors.Is(context.Cause(lease.Context()), admission.ErrExpired) {
			cancel()
		}
	})
	defer stopped()
	reader := traceframe.NewReader(source)
	first, err := reader.Next()
	if err != nil {
		return admission.ErrUnavailable
	}
	if first.MatchAdmission(a.ID(), a.EngineDigest(), digest, a.Specification(), lease.Deadline()) != nil {
		return admission.ErrTargetChanged
	}
	if err := lease.RevalidateStream(ctx); err != nil {
		return err
	}
	sink, err := streamhttp.NewSink(w)
	if err != nil {
		return admission.ErrUnavailable
	}
	watchCtx, stopWatch := context.WithCancel(ctx)
	defer stopWatch()
	done := make(chan struct{})
	validationFailure := make(chan error, 1)
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-watchCtx.Done():
				return
			case <-ticker.C:
				check, stop := context.WithTimeout(watchCtx, time.Second)
				err := lease.RevalidateStream(check)
				stop()
				if err != nil {
					validationFailure <- err
					cancel()
					return
				}
			}
		}
	}()
	defer func() { stopWatch(); <-done }()
	relay := &relay{reader: reader, sink: sink, lease: lease}
	if err := relay.run(ctx, first); err != nil {
		stopWatch()
		<-done
		termination := trace.EngineFailed
		select {
		case failed := <-validationFailure:
			termination = validationTermination(failed)
		default:
			if errors.Is(err, admission.ErrDenied) || errors.Is(err, admission.ErrTargetChanged) {
				termination = validationTermination(err)
			}
			if errors.Is(context.Cause(lease.Context()), context.Canceled) {
				termination = trace.Cancelled
			}
		}
		return relay.terminate(parent, termination)
	}
	return nil
}

type streamResponse struct {
	http.ResponseWriter
	started bool
}

func (w *streamResponse) WriteHeader(code int) { w.started = true; w.ResponseWriter.WriteHeader(code) }
func (w *streamResponse) Write(data []byte) (int, error) {
	w.started = true
	return w.ResponseWriter.Write(data)
}
func (w *streamResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (h *Handler) serveStream(w http.ResponseWriter, r *http.Request, principal user.Info, namespace, id string) {
	if r.Method != http.MethodGet || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		writeError(w, admission.ErrInvalidRequest)
		return
	}
	if h.stream == nil {
		writeError(w, admission.ErrUnavailable)
		return
	}
	select {
	case h.streamSlots <- struct{}{}:
		defer func() { <-h.streamSlots }()
	default:
		writeError(w, admission.ErrCapacity)
		return
	}
	lease, err := h.manager.Claim(r.Context(), principal, namespace, id)
	if err != nil {
		writeError(w, err)
		return
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = lease.Close(ctx)
	}()
	response := &streamResponse{ResponseWriter: w}
	if err := h.stream.serve(r.Context(), response, lease); err != nil {
		if response.started {
			panic(http.ErrAbortHandler)
		}
		writeError(w, err)
	}
}
