package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/aggregate"
	"github.com/danushkastanley/kube-memlens/internal/api"
	"k8s.io/client-go/rest"
)

const (
	aggregatedPageSize        = 500
	aggregatedMaxItems        = 100_000
	aggregatedMaxResponseSize = 16 << 20
)

type KubernetesAPIClient struct {
	baseURL    string
	httpClient *http.Client
	scope      ReadScope
}

var _ SnapshotReader = (*KubernetesAPIClient)(nil)
var _ CurrentSnapshotReader = (*KubernetesAPIClient)(nil)
var _ PodReader = (*KubernetesAPIClient)(nil)
var _ MetricsReader = (*KubernetesAPIClient)(nil)

func NewKubernetesAPIClient(config *rest.Config, scope ReadScope, timeout time.Duration) (*KubernetesAPIClient, error) {
	if config == nil {
		return nil, fmt.Errorf("Kubernetes rest config is required")
	}
	if strings.TrimSpace(config.Host) == "" {
		return nil, fmt.Errorf("Kubernetes API host is required")
	}
	if err := scope.validate(); err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	copied := rest.CopyConfig(config)
	copied.Timeout = timeout
	httpClient, err := rest.HTTPClientFor(copied)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes API transport: %w", err)
	}
	baseURL := strings.TrimRight(copied.Host, "/") + "/apis/" + api.MemoryAPIGroup + "/" + api.MemoryAPIVersion
	return &KubernetesAPIClient{baseURL: baseURL, httpClient: httpClient, scope: scope}, nil
}

func (c *KubernetesAPIClient) Health(ctx context.Context) error {
	return c.get(ctx, "discover KubeMemLens API", "", nil)
}

func (c *KubernetesAPIClient) Containers(ctx context.Context) ([]api.ContainerSnapshot, error) {
	path, err := c.scope.resourcePath("containers")
	if err != nil {
		return nil, err
	}
	items, err := loadAggregatedPages(ctx, c, "list containers", path,
		func() *api.ContainerMemoryList { return &api.ContainerMemoryList{} },
		func(list *api.ContainerMemoryList) ([]api.ContainerMemory, string) { return list.Items, list.Continue })
	if err != nil {
		return nil, err
	}
	snapshots := make([]api.ContainerSnapshot, len(items))
	for index := range items {
		snapshots[index] = items[index].Snapshot
	}
	return snapshots, nil
}

func (c *KubernetesAPIClient) Pods(ctx context.Context) ([]api.PodSnapshot, error) {
	path, err := c.scope.resourcePath("pods")
	if err != nil {
		return nil, err
	}
	items, err := loadAggregatedPages(ctx, c, "list pods", path,
		func() *api.PodMemoryList { return &api.PodMemoryList{} },
		func(list *api.PodMemoryList) ([]api.PodMemory, string) { return list.Items, list.Continue })
	if err != nil {
		return nil, err
	}
	snapshots := make([]api.PodSnapshot, len(items))
	for index := range items {
		snapshots[index] = items[index].Snapshot
	}
	return snapshots, nil
}

func (c *KubernetesAPIClient) Pod(ctx context.Context, namespace, podName string) (api.PodSnapshot, error) {
	path, err := c.namespacedObjectPath(namespace, "pods", podName)
	if err != nil {
		return api.PodSnapshot{}, err
	}
	var pod api.PodMemory
	if err := c.get(ctx, "get Pod", path, &pod); err != nil {
		return api.PodSnapshot{}, err
	}
	return pod.Snapshot, nil
}

func (c *KubernetesAPIClient) Namespaces(ctx context.Context) ([]api.NamespaceSnapshot, error) {
	pods, err := c.Pods(ctx)
	if err != nil {
		return nil, err
	}
	return aggregate.Namespaces(pods), nil
}

func (c *KubernetesAPIClient) Nodes(ctx context.Context) ([]api.NodeSnapshotStatus, error) {
	items, err := loadAggregatedPages(ctx, c, "list nodes", "/nodes",
		func() *api.NodeMemoryList { return &api.NodeMemoryList{} },
		func(list *api.NodeMemoryList) ([]api.NodeMemory, string) { return list.Items, list.Continue })
	if err != nil {
		return nil, err
	}
	snapshots := make([]api.NodeSnapshotStatus, len(items))
	for index := range items {
		snapshots[index] = items[index].Snapshot
	}
	return snapshots, nil
}

func (c *KubernetesAPIClient) Workloads(ctx context.Context) ([]api.WorkloadSnapshot, error) {
	path, err := c.scope.resourcePath("workloads")
	if err != nil {
		return nil, err
	}
	items, err := loadAggregatedPages(ctx, c, "list workloads", path,
		func() *api.WorkloadMemoryList { return &api.WorkloadMemoryList{} },
		func(list *api.WorkloadMemoryList) ([]api.WorkloadMemory, string) { return list.Items, list.Continue })
	if err != nil {
		return nil, err
	}
	snapshots := make([]api.WorkloadSnapshot, len(items))
	for index := range items {
		snapshots[index] = items[index].Snapshot
	}
	return snapshots, nil
}

func (c *KubernetesAPIClient) PodHistory(ctx context.Context, namespace, podName string) ([]api.PodHistory, error) {
	path, err := c.namespacedObjectPath(namespace, "pods", podName)
	if err != nil {
		return nil, err
	}
	path += "/history"
	var history api.PodMemoryHistory
	if err := c.get(ctx, "get Pod history", path, &history); err != nil {
		return nil, err
	}
	return history.Series, nil
}

func (c *KubernetesAPIClient) namespacedObjectPath(namespace, resource, name string) (string, error) {
	scope, err := NamespaceScope(namespace)
	if err != nil {
		return "", err
	}
	if !c.scope.allowsNamespace(scope.Namespace) {
		return "", &ReadError{Kind: ReadErrorForbidden, Operation: "build scoped request"}
	}
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "/") {
		return "", fmt.Errorf("resource name is required")
	}
	return "/namespaces/" + scope.Namespace + "/" + resource + "/" + url.PathEscape(name), nil
}

func (c *KubernetesAPIClient) DebugStore(ctx context.Context) (api.DebugStore, error) {
	var status api.ClusterStatus
	if err := c.get(ctx, "get cluster status", "/clusterstatus/current", &status); err != nil {
		return api.DebugStore{}, err
	}
	return status.Store, nil
}

func (c *KubernetesAPIClient) CurrentSnapshot(ctx context.Context) (CurrentSnapshot, error) {
	containers, err := c.Containers(ctx)
	if err != nil {
		return CurrentSnapshot{}, err
	}
	pods, err := c.Pods(ctx)
	if err != nil {
		return CurrentSnapshot{}, err
	}
	workloads, err := c.Workloads(ctx)
	if err != nil {
		return CurrentSnapshot{}, err
	}
	return CurrentSnapshot{
		Containers: containers,
		Pods:       pods,
		Namespaces: aggregate.Namespaces(pods),
		Workloads:  workloads,
	}, nil
}

func (c *KubernetesAPIClient) Metrics(ctx context.Context) (api.Metrics, error) {
	var result api.Metrics
	if err := c.get(ctx, "get metrics", "/metrics/current", &result); err != nil {
		return api.Metrics{}, err
	}
	return result, nil
}

func (c *KubernetesAPIClient) get(ctx context.Context, operation, path string, out any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return readTransportError(operation, err)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return readTransportError(operation, err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return readStatusError(operation, response.StatusCode)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, aggregatedMaxResponseSize+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return readDecodeError(operation, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return readDecodeError(operation, fmt.Errorf("unexpected trailing JSON"))
	}
	return nil
}

func loadAggregatedPages[Item any, List any](
	ctx context.Context,
	client *KubernetesAPIClient,
	operation string,
	basePath string,
	newList func() *List,
	page func(*List) ([]Item, string),
) ([]Item, error) {
	items := make([]Item, 0, aggregatedPageSize)
	continuation := ""
	seen := map[string]struct{}{}
	for {
		path := basePath + "?limit=" + fmt.Sprint(aggregatedPageSize)
		if continuation != "" {
			path += "&continue=" + url.QueryEscape(continuation)
		}
		list := newList()
		if err := client.get(ctx, operation, path, list); err != nil {
			return nil, err
		}
		pageItems, next := page(list)
		if len(items)+len(pageItems) > aggregatedMaxItems {
			return nil, readDecodeError(operation, fmt.Errorf("response exceeds %d items", aggregatedMaxItems))
		}
		items = append(items, pageItems...)
		if next == "" {
			return items, nil
		}
		if len(pageItems) == 0 {
			return nil, readDecodeError(operation, fmt.Errorf("empty page has a continuation token"))
		}
		if _, duplicate := seen[next]; duplicate {
			return nil, readDecodeError(operation, fmt.Errorf("repeated continuation token"))
		}
		seen[next] = struct{}{}
		continuation = next
	}
}
