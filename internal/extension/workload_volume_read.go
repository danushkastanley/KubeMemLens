package extension

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/aggregate"
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/collector"
	"github.com/danushkastanley/kube-memlens/internal/kube"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
)

func (h *ReadHandler) serveWorkloadVolumes(w http.ResponseWriter, r *http.Request, info *apirequest.RequestInfo, schema int) {
	resolver, ok := h.volumeResolver.(kube.WorkloadVolumeResolver)
	if !ok || !h.volumeWorkloadsEnabled || schema < api.IOPressureSnapshotSchemaVersion || !h.volumeNamespaces[info.Namespace] || info.Verb != "get" || len(info.Parts) != 3 {
		writeReadError(w, http.StatusNotFound, metav1.StatusReasonNotFound, "requested resource was not found")
		return
	}
	query := r.URL.Query()
	kind, valid := kube.CanonicalVolumeWorkloadKind(query.Get("kind"))
	if !valid || len(query) != 1 || len(query["kind"]) != 1 || len(validation.IsDNS1123Subdomain(info.Name)) != 0 {
		writeReadError(w, http.StatusBadRequest, metav1.StatusReasonBadRequest, "a supported workload kind and name are required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 9*time.Second)
	defer cancel()
	resolved, err := resolver.ResolveWorkload(ctx, info.Namespace, kind, info.Name)
	if err != nil {
		writeVolumeReadError(w, err)
		return
	}
	if resolved.Namespace != info.Namespace || resolved.Kind != kind || resolved.Name != info.Name {
		writeVolumeReadError(w, volumecontext.ErrScope)
		return
	}
	response, err := h.workloadVolumeResponse(ctx, resolved)
	if errors.Is(err, collector.ErrReadPageTooLarge) {
		err = kube.ErrVolumeWorkloadBounds
	}
	if err != nil {
		writeVolumeReadError(w, err)
		return
	}
	writeBoundedReadJSON(w, response, min(h.opts.MaxResponseBytes, volumecontext.MaxPageBytes))
}

func (h *ReadHandler) workloadVolumeResponse(ctx context.Context, resolved kube.ResolvedWorkloadVolumes) (api.WorkloadVolumeContext, error) {
	if len(resolved.Pods)+len(resolved.Unscheduled) > volumecontext.MaxWorkloadPods || resolved.UID == "" {
		return api.WorkloadVolumeContext{}, kube.ErrVolumeWorkloadBounds
	}
	response := api.WorkloadVolumeContext{TypeMeta: metav1.TypeMeta{APIVersion: readAPIVersion, Kind: "WorkloadVolumeContext"}, ObjectMeta: metav1.ObjectMeta{Namespace: resolved.Namespace, Name: resolved.Name, UID: types.UID(resolved.UID)}, ObservedAt: resolved.ObservedAt}
	for _, pod := range resolved.Unscheduled {
		if pod.Namespace != resolved.Namespace {
			return api.WorkloadVolumeContext{}, volumecontext.ErrScope
		}
		response.UnscheduledPods = append(response.UnscheduledPods, api.WorkloadUnscheduledPod{ObjectMeta: metav1.ObjectMeta{Namespace: pod.Namespace, Name: pod.PodName, UID: types.UID(pod.PodUID)}, Reason: "not-scheduled"})
	}
	expected := map[string]string{}
	views := make([]volumecontext.View, 0, len(resolved.Pods))
	for _, pod := range resolved.Pods {
		if pod.Scope.Namespace != resolved.Namespace || pod.Scope.PodUID == "" || expected[pod.Scope.PodName] != "" {
			return api.WorkloadVolumeContext{}, volumecontext.ErrScope
		}
		if err := h.authoriseVolumeObject(ctx, kube.VolumeAccess{Group: api.MemoryAPIGroup, Resource: "pods", Namespace: resolved.Namespace, Name: pod.Scope.PodName}); err != nil {
			return api.WorkloadVolumeContext{}, err
		}
		expected[pod.Scope.PodName] = pod.Scope.PodUID
		samples := volumecontext.Samples{State: volumecontext.SourceState(volumehealth.Disabled, volumecontext.Disabled)}
		var err error
		if h.volumeStatsEnabled {
			samples, err = h.store.VolumeSamples(pod.Scope, h.now())
			if err != nil {
				return api.WorkloadVolumeContext{}, err
			}
		}
		report, err := volumecontext.JoinSamples(pod.Scope, pod.Bindings, samples, pod.Health, h.now())
		if err != nil {
			return api.WorkloadVolumeContext{}, err
		}
		view := report.Authorised()
		views = append(views, view)
		response.PodVolumes = append(response.PodVolumes, api.PodVolumeContext{TypeMeta: metav1.TypeMeta{APIVersion: readAPIVersion, Kind: "PodVolumeContext"}, ObjectMeta: metav1.ObjectMeta{Namespace: resolved.Namespace, Name: pod.Scope.PodName, UID: types.UID(pod.Scope.PodUID)}, Context: view})
	}
	pods, err := h.store.WorkloadVolumePods(resolved.Namespace, expected, h.now(), h.opts.SnapshotTTL)
	if err != nil {
		return api.WorkloadVolumeContext{}, err
	}
	response.Workload = aggregate.SummariseWorkload(resolved.Namespace, resolved.Kind, resolved.Name, pods)
	if len(pods) != len(expected) || len(pods) == 0 || len(resolved.Unscheduled) > 0 {
		response.Workload.Completeness = api.EvidencePartial
	}
	response.Filesystems, err = volumecontext.GroupWorkload(views, h.now())
	if err != nil {
		return api.WorkloadVolumeContext{}, err
	}
	if ctx.Err() != nil {
		return api.WorkloadVolumeContext{}, ctx.Err()
	}
	return response, nil
}
