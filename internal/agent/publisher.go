package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/buildinfo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

type SnapshotPublisher struct {
	client  *http.Client
	baseURL *url.URL

	mu       sync.Mutex
	epoch    string
	sequence uint64
}

func NewSnapshotPublisher(config *rest.Config) (*SnapshotPublisher, error) {
	if config == nil {
		return nil, fmt.Errorf("Kubernetes config is required")
	}
	clientConfig := rest.CopyConfig(config)
	clientConfig.DisableCompression = true
	client, err := rest.HTTPClientFor(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("create authenticated ingestion client: %w", err)
	}
	return newSnapshotPublisher(client, config.Host)
}

func newSnapshotPublisher(client *http.Client, rawBaseURL string) (*SnapshotPublisher, error) {
	if client == nil {
		return nil, fmt.Errorf("authenticated HTTP client is required")
	}
	baseURL, err := url.Parse(strings.TrimRight(rawBaseURL, "/"))
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, fmt.Errorf("Kubernetes API URL is invalid")
	}
	return &SnapshotPublisher{client: client, baseURL: baseURL}, nil
}

func (p *SnapshotPublisher) Publish(ctx context.Context, nodeUID string, snapshot api.AgentSnapshot) error {
	if strings.TrimSpace(nodeUID) == "" {
		return fmt.Errorf("authenticated ingestion requires the Kubernetes node UID")
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.epoch == "" {
		if err := p.refreshEpoch(ctx); err != nil {
			return err
		}
	}
	if p.sequence == math.MaxUint64 {
		return fmt.Errorf("authenticated ingestion sequence is exhausted")
	}
	p.sequence++
	request := api.NodeSnapshotRequest{
		TypeMeta: metav1.TypeMeta{APIVersion: api.MemoryAPIGroup + "/" + api.MemoryAPIVersion, Kind: "NodeSnapshot"},
		NodeUID:  nodeUID,
		Epoch:    p.epoch,
		Sequence: p.sequence,
		Snapshot: snapshot,
	}
	response, apiErr, err := p.post(ctx, request)
	if err != nil || apiErr != nil && apiErr.Status == http.StatusUnauthorized {
		response, apiErr, err = p.post(ctx, request)
	}
	if err != nil {
		return err
	}
	if apiErr != nil && apiErr.Code == "epoch_mismatch" {
		if err := p.refreshEpoch(ctx); err != nil {
			return err
		}
		request.Epoch = p.epoch
		response, apiErr, err = p.post(ctx, request)
	}
	if err != nil {
		return err
	}
	if apiErr != nil {
		return fmt.Errorf("authenticated ingestion rejected request: code=%s status=%d", apiErr.Code, apiErr.Status)
	}
	if !response.Accepted || response.APIVersion != api.MemoryAPIGroup+"/"+api.MemoryAPIVersion || response.Kind != "NodeSnapshotResult" {
		return fmt.Errorf("authenticated ingestion returned an invalid success response")
	}
	return nil
}

func (p *SnapshotPublisher) refreshEpoch(ctx context.Context) error {
	var epoch api.IngestionEpoch
	status, err := p.requestJSON(ctx, http.MethodGet, ingestionPath("ingestionepochs/current"), nil, &epoch)
	if err != nil {
		return fmt.Errorf("get ingestion epoch: %w", err)
	}
	if status < 200 || status > 299 {
		return fmt.Errorf("get ingestion epoch: status=%d", status)
	}
	if epoch.APIVersion != api.MemoryAPIGroup+"/"+api.MemoryAPIVersion || epoch.Kind != "IngestionEpoch" || epoch.ObjectMeta.Name != "current" || epoch.Epoch == "" {
		return fmt.Errorf("get ingestion epoch: response is invalid")
	}
	p.epoch = epoch.Epoch
	if epoch.LastSequence > p.sequence {
		p.sequence = epoch.LastSequence
	}
	return nil
}

type responseError struct {
	Status int
	Code   string
}

func (p *SnapshotPublisher) post(ctx context.Context, request api.NodeSnapshotRequest) (api.NodeSnapshotResponse, *responseError, error) {
	var response api.NodeSnapshotResponse
	status, body, err := p.doJSON(ctx, http.MethodPost, ingestionPath("nodesnapshots"), request)
	if err != nil {
		return response, nil, fmt.Errorf("post authenticated snapshot: %w", err)
	}
	if status >= 200 && status <= 299 {
		if err := json.Unmarshal(body, &response); err != nil {
			return response, nil, fmt.Errorf("decode authenticated snapshot response: %w", err)
		}
		return response, nil, nil
	}
	var apiError metav1.Status
	if err := json.Unmarshal(body, &apiError); err != nil || apiError.Reason == "" {
		return response, &responseError{Status: status, Code: "unknown"}, nil
	}
	return response, &responseError{Status: status, Code: string(apiError.Reason)}, nil
}

func (p *SnapshotPublisher) requestJSON(ctx context.Context, method, path string, requestBody any, response any) (int, error) {
	status, body, err := p.doJSON(ctx, method, path, requestBody)
	if err != nil {
		return 0, err
	}
	if status >= 200 && status <= 299 {
		if err := json.Unmarshal(body, response); err != nil {
			return status, fmt.Errorf("decode response: %w", err)
		}
	}
	return status, nil
}

func (p *SnapshotPublisher) doJSON(ctx context.Context, method, path string, requestBody any) (int, []byte, error) {
	var body io.Reader
	if requestBody != nil {
		encoded, err := json.Marshal(requestBody)
		if err != nil {
			return 0, nil, fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	endpoint := p.baseURL.ResolveReference(&url.URL{Path: path})
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return 0, nil, fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "kube-memlens-agent/"+buildinfo.Version)
	if requestBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := p.client.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil {
		return 0, nil, fmt.Errorf("read response: %w", err)
	}
	if len(responseBody) > 64<<10 {
		return 0, nil, fmt.Errorf("response exceeds 65536 bytes")
	}
	return response.StatusCode, responseBody, nil
}

func ingestionPath(resource string) string {
	return "/apis/" + api.MemoryAPIGroup + "/" + api.MemoryAPIVersion + "/" + resource
}
