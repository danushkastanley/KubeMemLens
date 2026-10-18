package extension

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/buildinfo"
	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"github.com/danushkastanley/kube-memlens/internal/kube"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

type sessionPodIdentity interface {
	LookupPod(context.Context, string, string) (kube.SessionPodIdentity, error)
}

type sessionCaptureSource struct {
	reads    *ReadHandler
	identity sessionPodIdentity
}

func (s *sessionCaptureSource) authorise(ctx context.Context, p incidentsession.Principal, name string) error {
	for _, group := range []string{api.MemoryAPIGroup, ""} {
		err := s.reads.authoriseVolumeObject(ctx, kube.VolumeAccess{Group: group, Resource: "pods", Namespace: p.Namespace, Name: name})
		if err != nil {
			var readErr *kube.HealthReadError
			if errors.As(err, &readErr) && readErr.Reason == volumehealth.AccessDenied {
				return incidentsession.ErrDenied
			}
			return incidentsession.ErrUnavailable
		}
	}
	return nil
}

func (s *sessionCaptureSource) CapturePod(ctx context.Context, p incidentsession.Principal, name string) ([]byte, error) {
	if err := s.authorise(ctx, p, name); err != nil {
		return nil, err
	}
	before, err := s.identity.LookupPod(ctx, p.Namespace, name)
	if err != nil {
		return nil, err
	}
	now := s.reads.now()
	pod, found, err := s.reads.store.GetPodBounded(p.Namespace, name, now, s.reads.opts.SnapshotTTL, min(s.reads.nestedReadBudget(), incidentsession.MaxCaptureBytes))
	if err != nil || !found {
		return nil, incidentsession.ErrUnavailable
	}
	if pod.PodUID != before.UID || pod.NodeName != before.NodeName {
		return nil, incidentsession.ErrChanged
	}
	bundle := api.WithoutIOIncident(api.IncidentBundle{SchemaVersion: api.IncidentSchema([]api.PodSnapshot{pod}), CapturedAt: now.UTC(), ToolVersion: buildinfo.Current(runtime.Version(), runtime.GOOS, runtime.GOARCH).String(),
		Partial: true, Pods: []api.PodSnapshot{pod}, Caveats: []string{"Capture contains one authorised Pod; cluster-wide summaries and other Pods are omitted."}})
	data, err := json.Marshal(bundle)
	if err != nil || len(data) > incidentsession.MaxCaptureBytes {
		return nil, incidentsession.ErrCapacity
	}
	if err := s.authorise(ctx, p, name); err != nil {
		return nil, err
	}
	after, err := s.identity.LookupPod(ctx, p.Namespace, name)
	if err != nil {
		return nil, err
	}
	if after != before {
		return nil, incidentsession.ErrChanged
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return data, nil
}
