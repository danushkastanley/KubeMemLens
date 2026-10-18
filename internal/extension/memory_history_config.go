package extension

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/kube"
	"github.com/danushkastanley/kube-memlens/internal/promhistory"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

type MemoryHistoryOptions struct {
	Endpoint, Cluster, CAFile, BearerTokenFile string
	Namespaces                                 []string
	Nodes, Workloads, Markers                  bool
}

func (o *MemoryHistoryOptions) Validate() error {
	if o == nil {
		return nil
	}
	if o.Endpoint == "" || o.Cluster == "" || o.CAFile == "" || len(o.Namespaces) > 64 || (!o.Nodes && len(o.Namespaces) == 0) || (o.Workloads && len(o.Namespaces) == 0) {
		return errors.New("memory history requires a provider, CA bundle, cluster and explicit scopes")
	}
	if o.Markers && len(o.Namespaces) == 0 {
		return errors.New("workload change markers require explicit history namespaces")
	}
	seen := map[string]bool{}
	for _, namespace := range o.Namespaces {
		if len(validation.IsDNS1123Label(namespace)) != 0 || seen[namespace] {
			return errors.New("memory history requires distinct valid namespaces")
		}
		seen[namespace] = true
	}
	return nil
}

func (h *Handler) configureMemoryHistory(ctx context.Context, kubeconfig string) error {
	o := h.opts.MemoryHistory
	if o == nil {
		return nil
	}
	if err := o.Validate(); err != nil {
		return err
	}
	ca, err := historyMaterial(o.CAFile, 1<<20)
	if err != nil {
		return err
	}
	token, err := historyMaterial(o.BearerTokenFile, 16384)
	if err != nil {
		return err
	}
	remote, err := promhistory.New(promhistory.Options{URL: o.Endpoint, Cluster: o.Cluster, CAData: ca, BearerToken: strings.TrimSpace(string(token))})
	if err != nil {
		return errors.New("cannot configure verified Prometheus history transport")
	}
	config, err := kube.BuildConfig(kubeconfig, "")
	if err != nil {
		remote.Close()
		return errors.New("cannot configure history identity acquisition")
	}
	resolver, err := kube.NewMemoryHistoryResolver(config, h.reads.authoriseVolumeObject, h.coordinator.store.VolumeNodeUID)
	if err != nil {
		remote.Close()
		return errors.New("cannot configure verified history target resolver")
	}
	context.AfterFunc(ctx, remote.Close)
	s := &memoryHistoryService{resolver: resolver, local: h.coordinator.store.MemoryHistory(), remote: remote, namespaces: map[string]bool{}, nodes: o.Nodes, workloads: o.Workloads, gate: make(chan struct{}, 1)}
	if o.Markers {
		s.contextResolver, err = kube.NewMemoryHistoryContextResolver(config, h.reads.authoriseVolumeObject, h.coordinator.store.VolumeNodeUID)
		if err != nil {
			remote.Close()
			return err
		}
		s.markers, err = kube.NewChangeMarkerProvider(config, h.reads.authoriseVolumeObject)
		if err != nil {
			remote.Close()
			return err
		}
	}
	for _, ns := range o.Namespaces {
		s.namespaces[ns] = true
	}
	h.reads.memoryHistory = s
	return nil
}

func historyMaterial(path string, limit int64) ([]byte, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot read history trust material")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || len(data) == 0 || int64(len(data)) > limit {
		return nil, errors.New("invalid history trust material")
	}
	return data, nil
}

func (h *Handler) historyResources() []metav1.APIResource {
	o := h.opts.MemoryHistory
	if o == nil {
		return nil
	}
	var result []metav1.APIResource
	if len(o.Namespaces) > 0 {
		result = append(result, metav1.APIResource{Name: "pods/trends", Namespaced: true, Kind: "MemoryHistory", Verbs: metav1.Verbs{"get"}})
		if o.Markers {
			result = append(result, metav1.APIResource{Name: "pods/trends-context", Namespaced: true, Kind: "MemoryHistoryContext", Verbs: metav1.Verbs{"get"}})
		}
	}
	if o.Workloads {
		result = append(result, metav1.APIResource{Name: "workloads/trends", Namespaced: true, Kind: "MemoryHistory", Verbs: metav1.Verbs{"get"}})
		if o.Markers {
			result = append(result, metav1.APIResource{Name: "workloads/trends-context", Namespaced: true, Kind: "MemoryHistoryContext", Verbs: metav1.Verbs{"get"}})
		}
	}
	if o.Nodes {
		result = append(result, metav1.APIResource{Name: "nodes/trends", Namespaced: false, Kind: "MemoryHistory", Verbs: metav1.Verbs{"get"}})
	}
	return result
}
