package client

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type MemoryHistoryReader interface {
	MemoryHistory(context.Context, memoryhistory.Request, memoryhistory.Query) (memoryhistory.Report, error)
}

func (c *KubernetesAPIClient) MemoryHistory(ctx context.Context, request memoryhistory.Request, query memoryhistory.Query) (memoryhistory.Report, error) {
	if request.Scope != memoryhistory.Node && !c.scope.allowsNamespace(request.Namespace) {
		return memoryhistory.Report{}, &ReadError{Kind: ReadErrorForbidden, Operation: "read memory history"}
	}
	if err := request.Validate(); err != nil {
		return memoryhistory.Report{}, err
	}
	if err := query.Validate(time.Now().UTC()); err != nil {
		return memoryhistory.Report{}, err
	}
	path := memoryHistoryPath(request, query, "trends")
	var resource struct {
		metav1.TypeMeta   `json:",inline"`
		metav1.ObjectMeta `json:"metadata"`
		History           memoryhistory.Report `json:"history"`
	}
	// History includes bounded provider work and identity revalidation. Keep its
	// ten-second transport budget separate from ordinary five-second live reads;
	// an earlier caller cancellation or deadline still takes precedence.
	historyClient := *c
	historyClient.httpClient = &http.Client{Transport: c.httpClient.Transport, CheckRedirect: c.httpClient.CheckRedirect, Jar: c.httpClient.Jar, Timeout: 10 * time.Second}
	if err := historyClient.getBounded(ctx, "read memory history", path, &resource, memoryhistory.MaxResponseBytes); err != nil {
		return memoryhistory.Report{}, err
	}
	r := resource.History
	if resource.Kind != "MemoryHistory" || resource.APIVersion != "memory.kubememlens.io/v1alpha1" || resource.Namespace != request.Namespace || resource.Name != request.Name || string(resource.UID) != r.Selection.UID || r.Selection.Request != request || r.Query.Source != query.Source || r.Query.Metric != query.Metric || r.Query.Step != query.Step || !r.Query.Start.Equal(query.Start) || !r.Query.End.Equal(query.End) {
		return memoryhistory.Report{}, readDecodeError("read memory history", fmt.Errorf("memory history response does not match the selected source and scope"))
	}
	if r.ReceivedAt.After(time.Now().UTC().Add(time.Minute)) {
		return memoryhistory.Report{}, readDecodeError("read memory history", memoryhistory.ErrInvalid)
	}
	if err := r.Validate(); err != nil {
		return memoryhistory.Report{}, readDecodeError("read memory history", err)
	}
	if ctx.Err() != nil {
		return memoryhistory.Report{}, ctx.Err()
	}
	return r, nil
}
