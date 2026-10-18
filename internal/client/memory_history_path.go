package client

import (
	"net/url"
	"strconv"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

func memoryHistoryPath(request memoryhistory.Request, query memoryhistory.Query, subresource string) string {
	values := url.Values{"source": {string(query.Source)}, "metric": {string(query.Metric)}, "start": {query.Start.UTC().Format(time.RFC3339)}, "end": {query.End.UTC().Format(time.RFC3339)}, "step": {strconv.FormatInt(int64(query.Step/time.Second), 10)}}
	path := "/namespaces/" + url.PathEscape(request.Namespace) + "/pods/" + url.PathEscape(request.Name) + "/" + subresource
	switch request.Scope {
	case memoryhistory.Container:
		values.Set("container", request.Container)
	case memoryhistory.Workload:
		path = "/namespaces/" + url.PathEscape(request.Namespace) + "/workloads/" + url.PathEscape(request.Name) + "/" + subresource
		values.Set("kind", request.WorkloadKind)
	case memoryhistory.Node:
		path = "/nodes/" + url.PathEscape(request.Name) + "/" + subresource
	}
	return path + "?" + values.Encode()
}
