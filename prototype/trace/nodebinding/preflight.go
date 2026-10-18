package nodebinding

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"io"
	"net/http"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/internal/tracepreflight"
)

const bindFields = "id|expires|namespace|pod|podUID|container|containerID|started|nodeUID|nodeName|qos|kind|paths|durationNanos|events|outputBytes|mapBytes|pathBytes"

// SetStartupReport retains validated bytes, never the caller's mutable Checks
// slice. Call once before serving; a report cannot be replaced during operation.
func (s *Service) SetStartupReport(report tracepreflight.Report) error {
	data, err := tracepreflight.Encode(report)
	if err != nil || report.State != tracepreflight.Supported {
		return admission.ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.startupReport != nil {
		return admission.ErrUnavailable
	}
	s.startupReport = data
	return nil
}

func (s *Service) servePreflight(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	var intent bindRequest
	if r.Header.Get("Content-Type") != "application/json" || decode(http.MaxBytesReader(w, r.Body, maxBody), &intent, bindFields) != nil || intent.ID != "" || !intent.Expires.IsZero() {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	result, err := s.inspectTarget(ctx, intent)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (s *Service) inspectTarget(ctx context.Context, r bindRequest) (result admission.NodePreflight, resultErr error) {
	w := r.workload()
	if w.Target.ValidateLifetime() != nil || r.NodeUID != s.nodeUID || r.NodeName != s.nodeName || trace.ValidateIntent(r.Kind, r.Paths, r.bounds()) != nil {
		return result, admission.ErrTargetChanged
	}
	s.mu.Lock()
	if s.closed || s.cleanupErr != nil || s.runtime == nil || s.startupReport == nil || ctx.Err() != nil {
		s.mu.Unlock()
		return result, admission.ErrUnavailable
	}
	if s.previewDone != nil {
		s.mu.Unlock()
		return result, admission.ErrCapacity
	}
	done := make(chan struct{})
	s.previewDone = done
	observed := s.startupReport
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		close(done)
		s.previewDone = nil
		s.mu.Unlock()
	}()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	defer cancel()
	profile, err := s.preflight(ctx)
	if err != nil || profile != tracepreflight.Baseline().Digest() {
		return result, admission.ErrUnavailable
	}
	handle, err := s.resolve(ctx, w)
	if handle == nil {
		return result, admission.ErrTargetChanged
	}
	defer func() {
		if handle.Close() != nil {
			s.mu.Lock()
			s.cleanupErr = admission.ErrUnavailable
			s.mu.Unlock()
			s.audit("cleanup_unconfirmed")
			result, resultErr = admission.NodePreflight{}, admission.ErrUnavailable
		}
	}()
	if err != nil {
		return result, admission.ErrTargetChanged
	}
	target := handle.Target()
	expected := w.Target
	expected.CgroupID = target.CgroupID
	if target != expected || target.CgroupID == 0 || handle.Check(ctx) != nil {
		return result, admission.ErrTargetChanged
	}
	spec, err := trace.NewSpecification(r.Kind, target, r.Paths, r.bounds())
	if err != nil {
		return result, admission.ErrTargetChanged
	}
	prepared, err := s.runtime.Prepare(ctx, spec, handle)
	if err != nil || prepared.Engine == nil || ctx.Err() != nil {
		return result, admission.ErrUnavailable
	}
	report, err := tracepreflight.Decode(observed)
	if err != nil {
		return result, admission.ErrUnavailable
	}
	result = admission.NodePreflight{Baseline: report, EngineDigest: prepared.EngineDigest, ProgrammeDigest: prepared.ProgrammeDigest, StreamVersion: prepared.StreamVersion}
	if result.Validate(r.Kind) != nil {
		return admission.NodePreflight{}, admission.ErrUnavailable
	}
	s.mu.Lock()
	unavailable := s.closed || s.cleanupErr != nil || ctx.Err() != nil
	s.mu.Unlock()
	if unavailable {
		return admission.NodePreflight{}, admission.ErrUnavailable
	}
	return result, nil
}

func (c *Client) Preflight(ctx context.Context, workload admission.Workload, intent admission.Request) (admission.NodePreflight, error) {
	n, ok := c.nodes[workload.Target.NodeUID]
	if !ok || n.name != workload.NodeName || workload.Target.CgroupID != 0 || workload.Target.ValidateLifetime() != nil || intent.Namespace() != workload.Target.Namespace || intent.Pod() != workload.Target.PodName || intent.Container() != workload.Target.ContainerName {
		return admission.NodePreflight{}, admission.ErrTargetChanged
	}
	instance, err := n.identity(ctx, workload.Target.NodeUID)
	if err != nil {
		return admission.NodePreflight{}, err
	}
	data, err := json.Marshal(requestFor("", workload, intent, time.Time{}))
	if err != nil {
		return admission.NodePreflight{}, admission.ErrUnavailable
	}
	response, err := n.call(ctx, http.MethodPost, "/v1/preflight", data, instance)
	if err != nil {
		return admission.NodePreflight{}, err
	}
	defer response.Body.Close()
	data, err = io.ReadAll(io.LimitReader(response.Body, tracepreflight.MaxReportBytes+1025))
	var result admission.NodePreflight
	if err != nil || len(data) > tracepreflight.MaxReportBytes+1024 || jsonv2.Unmarshal(data, &result, jsonv2.RejectUnknownMembers(true)) != nil {
		return admission.NodePreflight{}, admission.ErrUnavailable
	}
	if result.Validate(intent.Kind()) != nil {
		return admission.NodePreflight{}, admission.ErrUnavailable
	}
	return result, nil
}
