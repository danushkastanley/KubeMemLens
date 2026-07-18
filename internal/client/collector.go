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

	"github.com/danushkastanley/kube-memlens/internal/api"
)

type CollectorClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

var _ SnapshotReader = (*CollectorClient)(nil)

func NewCollectorClient(baseURL string) *CollectorClient {
	return NewCollectorClientWithTimeout(baseURL, defaultTimeout)
}

func NewCollectorClientWithTimeout(baseURL string, timeout time.Duration) *CollectorClient {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &CollectorClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *CollectorClient) Health(ctx context.Context) error {
	return c.get(ctx, "/healthz", nil)
}

func (c *CollectorClient) Containers(ctx context.Context) ([]api.ContainerSnapshot, error) {
	snapshot, err := c.CurrentSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return snapshot.Containers, nil
}

func (c *CollectorClient) Pods(ctx context.Context) ([]api.PodSnapshot, error) {
	snapshot, err := c.CurrentSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return snapshot.Pods, nil
}

func (c *CollectorClient) Namespaces(ctx context.Context) ([]api.NamespaceSnapshot, error) {
	snapshot, err := c.CurrentSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return snapshot.Namespaces, nil
}

func (c *CollectorClient) Nodes(ctx context.Context) ([]api.NodeSnapshotStatus, error) {
	var out []api.NodeSnapshotStatus
	if err := c.get(ctx, "/api/v1/nodes", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *CollectorClient) Workloads(ctx context.Context) ([]api.WorkloadSnapshot, error) {
	snapshot, err := c.CurrentSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return snapshot.Workloads, nil
}

func (c *CollectorClient) CurrentSnapshot(ctx context.Context) (CurrentSnapshot, error) {
	return loadCurrentSnapshot(ctx, c.get)
}

func (c *CollectorClient) PodHistory(ctx context.Context, namespace, podName string) ([]api.PodHistory, error) {
	var out []api.PodHistory
	path := "/api/v1/history/pods/" + url.PathEscape(namespace) + "/" + url.PathEscape(podName)
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *CollectorClient) DebugStore(ctx context.Context) (api.DebugStore, error) {
	var out api.DebugStore
	if err := c.get(ctx, "/api/v1/debug/store", &out); err != nil {
		return api.DebugStore{}, err
	}
	return out, nil
}

func (c *CollectorClient) endpoint(path string) (string, error) {
	if strings.TrimSpace(c.BaseURL) == "" {
		return "", fmt.Errorf("collector base URL is required")
	}
	return c.BaseURL + path, nil
}

func (c *CollectorClient) get(ctx context.Context, path string, out any) error {
	endpoint, err := c.endpoint(path)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build GET %s: %w", endpoint, err)
	}

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = (&CollectorClient{}).HTTPClient
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("GET %s: status %d body=%q", endpoint, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}

	decoder := json.NewDecoder(resp.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", endpoint, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("decode %s: unexpected trailing JSON", endpoint)
	}
	return nil
}
