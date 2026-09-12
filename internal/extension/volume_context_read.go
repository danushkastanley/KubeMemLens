package extension

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/kube"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
)

func VolumeNamespaces(value string) ([]string, error) {
	if value == "" {
		return nil, nil
	}
	values := strings.Split(value, ",")
	if len(values) > 64 {
		return nil, errors.New("volume context supports at most 64 configured namespaces")
	}
	seen := map[string]bool{}
	for _, name := range values {
		if len(validation.IsDNS1123Label(name)) != 0 || seen[name] {
			return nil, errors.New("volume context requires distinct Kubernetes namespace names")
		}
		seen[name] = true
	}
	return values, nil
}

func (h *ReadHandler) authoriseVolumeObject(ctx context.Context, a kube.VolumeAccess) error {
	principal, ok := apirequest.UserFrom(ctx)
	if !ok || principal == nil || h.podAuthorizer == nil {
		return &kube.HealthReadError{Reason: volumehealth.AccessDenied}
	}
	decision, _, err := h.podAuthorizer.Authorize(ctx, authorizer.AttributesRecord{User: principal, Verb: "get", APIGroup: a.Group, APIVersion: "v1", Resource: a.Resource, Namespace: a.Namespace, Name: a.Name, ResourceRequest: true})
	if err != nil {
		return &kube.HealthReadError{Reason: volumehealth.ReadFailed}
	}
	if decision != authorizer.DecisionAllow {
		return &kube.HealthReadError{Reason: volumehealth.AccessDenied}
	}
	return nil
}

func (h *ReadHandler) servePodVolumes(w http.ResponseWriter, r *http.Request, info *apirequest.RequestInfo, schema int) {
	if schema < api.VolumeSnapshotSchemaVersion || h.volumeResolver == nil || !h.volumeNamespaces[info.Namespace] || info.Name == "" || info.Verb != "get" || len(info.Parts) != 3 {
		writeReadError(w, http.StatusNotFound, metav1.StatusReasonNotFound, "requested resource was not found")
		return
	}
	resolved, err := h.volumeResolver.Resolve(r.Context(), info.Namespace, info.Name)
	if err != nil {
		writeVolumeReadError(w, err)
		return
	}
	if resolved.Scope.Namespace != info.Namespace || resolved.Scope.PodName != info.Name {
		writeVolumeReadError(w, volumecontext.ErrScope)
		return
	}
	samples := volumecontext.Samples{State: volumecontext.SourceState(volumehealth.Disabled, volumecontext.Disabled)}
	if h.volumeStatsEnabled {
		samples, err = h.store.VolumeSamples(resolved.Scope, h.now())
		if err != nil {
			writeVolumeReadError(w, err)
			return
		}
	}
	result, err := volumecontext.JoinSamples(resolved.Scope, resolved.Bindings, samples, resolved.Health, h.now())
	if err != nil {
		writeVolumeReadError(w, err)
		return
	}
	response := api.PodVolumeContext{TypeMeta: metav1.TypeMeta{APIVersion: readAPIVersion, Kind: "PodVolumeContext"},
		ObjectMeta: metav1.ObjectMeta{Namespace: info.Namespace, Name: info.Name, UID: types.UID(resolved.Scope.PodUID)}, Context: result.Authorised()}
	writeBoundedReadJSON(w, api.PodVolumeContextForSchema(response, schema), min(h.opts.MaxResponseBytes, volumecontext.MaxPageBytes))
}

func writeVolumeReadError(w http.ResponseWriter, err error) {
	if errors.Is(err, kube.ErrVolumePodNotFound) {
		writeReadError(w, http.StatusNotFound, metav1.StatusReasonNotFound, "requested resource was not found")
		return
	}
	var failure *kube.HealthReadError
	if errors.As(err, &failure) && failure.Reason == volumehealth.AccessDenied {
		writeReadError(w, http.StatusForbidden, metav1.StatusReasonForbidden, "volume context access is denied")
		return
	}
	writeReadError(w, http.StatusServiceUnavailable, metav1.StatusReasonServiceUnavailable, "current volume context is unavailable")
}

func (h *Handler) configureVolumeResolver(ctx context.Context, kubeconfig string) error {
	if len(h.opts.VolumeNamespaces) == 0 {
		return nil
	}
	config, err := kube.BuildConfig(kubeconfig, "")
	if err != nil {
		return errors.New("cannot configure volume context acquisition")
	}
	var resolver kube.VolumeResolver
	if h.opts.VolumeHealthEnabled {
		resolver, err = kube.NewHealthVolumeResolver(ctx, config, h.reads.authoriseVolumeObject, h.coordinator.store.VolumeNodeUID)
	} else {
		resolver, err = kube.NewVolumeResolver(config, h.reads.authoriseVolumeObject, h.coordinator.store.VolumeNodeUID)
	}
	if err != nil {
		return errors.New("cannot configure verified volume context resolver")
	}
	h.reads.volumeResolver = resolver
	return nil
}
