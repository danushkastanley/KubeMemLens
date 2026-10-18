package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"github.com/danushkastanley/kube-memlens/internal/replicabaseline"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ReplicaReader interface {
	ReplicaBaseline(context.Context, memoryhistory.Request) (replicabaseline.Report, error)
}

func (c *KubernetesAPIClient) ReplicaBaseline(ctx context.Context, request memoryhistory.Request) (replicabaseline.Report, error) {
	const operation = "read replica baseline"
	if !c.scope.allowsNamespace(request.Namespace) {
		return replicabaseline.Report{}, &ReadError{Kind: ReadErrorForbidden, Operation: operation}
	}
	if request.Validate() != nil || (request.Scope != memoryhistory.Pod && request.Scope != memoryhistory.Workload) {
		return replicabaseline.Report{}, replicabaseline.ErrInvalid
	}
	path := "/namespaces/" + url.PathEscape(request.Namespace) + "/pods/" + url.PathEscape(request.Name) + "/replicas"
	if request.Scope == memoryhistory.Workload {
		path = "/namespaces/" + url.PathEscape(request.Namespace) + "/workloads/" + url.PathEscape(request.Name) + "/replicas?" + url.Values{"kind": {request.WorkloadKind}}.Encode()
	}
	var resource struct {
		metav1.TypeMeta   `json:",inline"`
		metav1.ObjectMeta `json:"metadata"`
		Requested         changemarkers.Object   `json:"requested"`
		Baseline          replicabaseline.Report `json:"baseline"`
	}
	transport := *c
	transport.httpClient = &http.Client{Transport: c.httpClient.Transport, CheckRedirect: c.httpClient.CheckRedirect, Jar: c.httpClient.Jar, Timeout: 10 * time.Second}
	if err := transport.getBounded(ctx, operation, path, &resource, replicabaseline.MaxResponseBytes); err != nil {
		return replicabaseline.Report{}, err
	}
	r, selected := resource.Baseline, resource.Requested
	kind := "Pod"
	if request.Scope == memoryhistory.Workload {
		kind = request.WorkloadKind
	}
	if resource.Kind != "ReplicaBaseline" || resource.APIVersion != "memory.kubememlens.io/v1alpha1" || resource.Namespace != request.Namespace || resource.Name != request.Name || string(resource.UID) != selected.UID || selected.Validate() != nil || selected.Namespace != request.Namespace || selected.Name != request.Name || selected.Kind != kind || r.Workload.Namespace != request.Namespace {
		return replicabaseline.Report{}, readDecodeError(operation, fmt.Errorf("replica evidence does not match the selected scope"))
	}
	if err := r.Validate(); err != nil {
		return replicabaseline.Report{}, readDecodeError(operation, err)
	}
	if request.Scope == memoryhistory.Pod {
		found := false
		for _, peer := range r.Peers {
			found = found || peer.Peer == selected
		}
		if !found {
			return replicabaseline.Report{}, readDecodeError(operation, replicabaseline.ErrScope)
		}
	}
	now := time.Now().UTC()
	if r.ObservedAt.After(now.Add(replicabaseline.MaximumSampleSkew)) || now.Sub(r.ObservedAt) > replicabaseline.FreshFor {
		return replicabaseline.Report{}, readDecodeError(operation, fmt.Errorf("replica evidence is no longer current"))
	}
	if err := ctx.Err(); err != nil {
		return replicabaseline.Report{}, err
	}
	return r, nil
}
