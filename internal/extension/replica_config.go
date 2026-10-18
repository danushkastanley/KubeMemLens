package extension

import (
	"errors"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/kube"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

type ReplicaOptions struct{ Namespaces []string }

func (o *ReplicaOptions) Validate() error {
	if o == nil {
		return nil
	}
	if len(o.Namespaces) == 0 || len(o.Namespaces) > 64 {
		return errors.New("replica comparisons require between 1 and 64 explicit namespaces")
	}
	seen := map[string]bool{}
	for _, ns := range o.Namespaces {
		if len(validation.IsDNS1123Label(ns)) != 0 || seen[ns] {
			return errors.New("replica comparisons require distinct valid namespaces")
		}
		seen[ns] = true
	}
	return nil
}

func (h *Handler) configureReplicas(kubeconfig string) error {
	o := h.opts.Replicas
	if o == nil {
		return nil
	}
	if err := o.Validate(); err != nil {
		return err
	}
	config, err := kube.BuildConfig(kubeconfig, "")
	if err != nil {
		return errors.New("cannot configure replica identity acquisition")
	}
	resolver, err := kube.NewReplicaResolver(config, h.reads.authoriseVolumeObject, h.coordinator.store.VolumeNodeUID)
	if err != nil {
		return err
	}
	service := &replicaService{resolver: resolver, evidence: h.coordinator.store.ReplicaEvidence, namespaces: map[string]bool{}, gate: make(chan struct{}, 1)}
	for _, ns := range o.Namespaces {
		service.namespaces[ns] = true
	}
	h.coordinator.store.EnableReplicaComparisons()
	h.reads.replicas = service
	return nil
}

func (h *Handler) replicaResources() []metav1.APIResource {
	if h.opts.Replicas == nil {
		return nil
	}
	result := []metav1.APIResource{}
	for _, resource := range []string{"pods", "workloads"} {
		result = append(result, metav1.APIResource{Name: strings.Join([]string{resource, "replicas"}, "/"), Namespaced: true, Kind: "ReplicaBaseline", Verbs: metav1.Verbs{"get"}})
	}
	return result
}
