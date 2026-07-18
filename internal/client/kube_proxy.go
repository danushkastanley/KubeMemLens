package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type KubeProxyCollectorClient struct {
	Namespace string
	Service   string
	Port      int

	restClient rest.Interface
	timeout    time.Duration

	mu                 sync.RWMutex
	workingServiceName string
}

var _ SnapshotReader = (*KubeProxyCollectorClient)(nil)

func NewKubeProxyCollectorClient(config *rest.Config, namespace string, service string, port int, timeout time.Duration) (*KubeProxyCollectorClient, error) {
	if config == nil {
		return nil, fmt.Errorf("kubernetes rest config is required")
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	copied := rest.CopyConfig(config)
	copied.Timeout = timeout

	kubeClient, err := kubernetes.NewForConfig(copied)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}
	return &KubeProxyCollectorClient{
		Namespace:  namespace,
		Service:    service,
		Port:       port,
		restClient: kubeClient.CoreV1().RESTClient(),
		timeout:    timeout,
	}, nil
}

func (c *KubeProxyCollectorClient) Health(ctx context.Context) error {
	body, err := c.getRaw(ctx, "/healthz")
	if err != nil {
		return err
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed != "" && trimmed != "ok" {
		return fmt.Errorf("GET /healthz via Kubernetes service proxy: unexpected body %q", trimmed)
	}
	return nil
}

func (c *KubeProxyCollectorClient) Containers(ctx context.Context) ([]api.ContainerSnapshot, error) {
	snapshot, err := c.CurrentSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return snapshot.Containers, nil
}

func (c *KubeProxyCollectorClient) Pods(ctx context.Context) ([]api.PodSnapshot, error) {
	snapshot, err := c.CurrentSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return snapshot.Pods, nil
}

func (c *KubeProxyCollectorClient) Namespaces(ctx context.Context) ([]api.NamespaceSnapshot, error) {
	snapshot, err := c.CurrentSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return snapshot.Namespaces, nil
}

func (c *KubeProxyCollectorClient) Nodes(ctx context.Context) ([]api.NodeSnapshotStatus, error) {
	var out []api.NodeSnapshotStatus
	if err := c.getJSON(ctx, "/api/v1/nodes", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *KubeProxyCollectorClient) Workloads(ctx context.Context) ([]api.WorkloadSnapshot, error) {
	snapshot, err := c.CurrentSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return snapshot.Workloads, nil
}

func (c *KubeProxyCollectorClient) CurrentSnapshot(ctx context.Context) (CurrentSnapshot, error) {
	return loadCurrentSnapshot(ctx, c.getJSON)
}

func (c *KubeProxyCollectorClient) PodHistory(ctx context.Context, namespace, podName string) ([]api.PodHistory, error) {
	var out []api.PodHistory
	path := "/api/v1/history/pods/" + url.PathEscape(namespace) + "/" + url.PathEscape(podName)
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *KubeProxyCollectorClient) DebugStore(ctx context.Context) (api.DebugStore, error) {
	var out api.DebugStore
	if err := c.getJSON(ctx, "/api/v1/debug/store", &out); err != nil {
		return api.DebugStore{}, err
	}
	return out, nil
}

func (c *KubeProxyCollectorClient) getJSON(ctx context.Context, path string, out any) error {
	body, err := c.getRaw(ctx, path)
	if err != nil {
		return err
	}
	return decodeKubeProxyJSON(path, body, out)
}

func decodeKubeProxyJSON(path string, body []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("decode %s via Kubernetes service proxy: %w", path, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("decode %s via Kubernetes service proxy: unexpected trailing JSON", path)
	}
	return nil
}

func (c *KubeProxyCollectorClient) getRaw(ctx context.Context, path string) ([]byte, error) {
	names := c.serviceNamesToTry()
	var attempted []string
	var lastErr error
	for _, name := range names {
		body, err := c.doRaw(ctx, name, path)
		attempted = append(attempted, name)
		if err == nil {
			c.setWorkingServiceName(name)
			return body, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("GET %s through service proxy failed for %s/%s:%d; attempted service names: %s: %w",
		path,
		c.Namespace,
		c.Service,
		c.Port,
		strings.Join(attempted, ", "),
		lastErr,
	)
}

func (c *KubeProxyCollectorClient) doRaw(ctx context.Context, serviceName string, path string) ([]byte, error) {
	parsed, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("parse collector path %q: %w", path, err)
	}
	req := c.restClient.Get().
		Namespace(c.Namespace).
		Resource("services").
		Name(serviceName).
		SubResource("proxy").
		Suffix(collectorPathParts(parsed.Path)...)
	for key, values := range parsed.Query() {
		for _, value := range values {
			req.Param(key, value)
		}
	}
	if c.timeout > 0 {
		req.Timeout(c.timeout)
	}
	return req.DoRaw(ctx)
}

func (c *KubeProxyCollectorClient) serviceNamesToTry() []string {
	candidates := serviceProxyNameCandidates(c.Service, c.Port)
	cached := c.cachedServiceName()
	if cached == "" {
		return candidates
	}
	names := []string{cached}
	for _, candidate := range candidates {
		if candidate != cached {
			names = append(names, candidate)
		}
	}
	return names
}

func (c *KubeProxyCollectorClient) cachedServiceName() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.workingServiceName
}

func (c *KubeProxyCollectorClient) setWorkingServiceName(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.workingServiceName = name
}

func serviceProxyNameCandidates(service string, port int) []string {
	service = strings.TrimSpace(service)
	if service == "" {
		return nil
	}
	if port <= 0 {
		return []string{service}
	}
	portText := strconv.Itoa(port)
	return []string{
		"http:" + service + ":" + portText,
		service + ":" + portText,
		service,
	}
}

func collectorPathParts(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func serviceProxyRequestPath(namespace string, serviceName string, collectorPath string) string {
	parts := []string{"api", "v1", "namespaces", namespace, "services", serviceName, "proxy"}
	parts = append(parts, collectorPathParts(collectorPath)...)
	return "/" + strings.Join(parts, "/")
}
