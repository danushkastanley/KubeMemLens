package client

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type MemoryHistoryContextReader interface {
	MemoryHistoryContext(context.Context, memoryhistory.Request, memoryhistory.Query) (changemarkers.Context, error)
}

func (c *KubernetesAPIClient) MemoryHistoryContext(ctx context.Context, request memoryhistory.Request, query memoryhistory.Query) (changemarkers.Context, error) {
	const operation = "read memory history context"
	if request.Scope == memoryhistory.Node || !c.scope.allowsNamespace(request.Namespace) {
		return changemarkers.Context{}, &ReadError{Kind: ReadErrorForbidden, Operation: operation}
	}
	if err := request.Validate(); err != nil {
		return changemarkers.Context{}, err
	}
	if err := query.Validate(time.Now().UTC()); err != nil {
		return changemarkers.Context{}, err
	}
	var resource struct {
		metav1.TypeMeta   `json:",inline"`
		metav1.ObjectMeta `json:"metadata"`
		Context           changemarkers.Context `json:"context"`
	}
	transport := *c
	transport.httpClient = &http.Client{Transport: c.httpClient.Transport, CheckRedirect: c.httpClient.CheckRedirect, Jar: c.httpClient.Jar, Timeout: 13 * time.Second}
	if err := transport.getBounded(ctx, operation, memoryHistoryPath(request, query, "trends-context"), &resource, memoryhistory.MaxResponseBytes); err != nil {
		return changemarkers.Context{}, err
	}
	value := resource.Context
	r := value.History
	if resource.Kind != "MemoryHistoryContext" || resource.APIVersion != "memory.kubememlens.io/v1alpha1" || resource.Namespace != request.Namespace || resource.Name != request.Name || string(resource.UID) != r.Selection.UID || r.Selection.Request != request || r.Query.Source != query.Source || r.Query.Metric != query.Metric || r.Query.Step != query.Step || !r.Query.Start.Equal(query.Start) || !r.Query.End.Equal(query.End) {
		return changemarkers.Context{}, readDecodeError(operation, fmt.Errorf("memory context does not match the selected source and scope"))
	}
	now := time.Now().UTC()
	if r.ReceivedAt.After(now.Add(time.Minute)) || value.Changes.ObservedAt.After(now.Add(time.Minute)) {
		return changemarkers.Context{}, readDecodeError(operation, changemarkers.ErrInvalid)
	}
	if err := value.Validate(); err != nil {
		return changemarkers.Context{}, readDecodeError(operation, err)
	}
	if err := ctx.Err(); err != nil {
		return changemarkers.Context{}, err
	}
	return value, nil
}
