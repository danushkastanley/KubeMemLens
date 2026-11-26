package nodebinding

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/internal/tracepreflight"
)

// Endpoint is trusted installation configuration keyed by immutable Node UID.
// No URL, certificate or Node name comes from the end-user request.
type Endpoint struct {
	NodeUID, NodeName, URL string
	Peer                   Peer
}
type nodeClient struct {
	name, url string
	owner     string
	http      *http.Client
	stream    *http.Client
}
type Client struct{ nodes map[string]nodeClient }

func NewClient(identity tls.Certificate, endpoints []Endpoint) (*Client, error) {
	if len(endpoints) == 0 || len(endpoints) > 64 {
		return nil, admission.ErrUnavailable
	}
	owner, err := newInstance()
	if err != nil {
		return nil, err
	}
	c := &Client{nodes: map[string]nodeClient{}}
	for _, e := range endpoints {
		if e.NodeUID == "" || e.NodeName == "" {
			return nil, admission.ErrUnavailable
		}
		if _, exists := c.nodes[e.NodeUID]; exists {
			return nil, admission.ErrUnavailable
		}
		h, err := newHTTPClient(e.URL, identity, e.Peer)
		if err != nil {
			return nil, err
		}
		c.nodes[e.NodeUID] = nodeClient{name: e.NodeName, url: e.URL, owner: owner, http: h, stream: newStreamClient(h)}
	}
	return c, nil
}
func (c *Client) Close() {
	for _, n := range c.nodes {
		n.http.CloseIdleConnections()
		n.stream.CloseIdleConnections()
	}
}

func (c *Client) Bind(ctx context.Context, id string, w admission.Workload, intent admission.Request, expires time.Time) (admission.Binding, error) {
	n, ok := c.nodes[w.Target.NodeUID]
	if !ok || intent.Namespace() != w.Target.Namespace || intent.Pod() != w.Target.PodName || intent.Container() != w.Target.ContainerName || trace.ValidateIntent(intent.Kind(), intent.Paths(), intent.Bounds()) != nil || n.name != w.NodeName || !validID(id) || w.Target.CgroupID != 0 || w.Target.ValidateLifetime() != nil || time.Until(expires) <= 0 || time.Until(expires) > maxLease {
		return nil, admission.ErrTargetChanged
	}
	instance, err := n.identity(ctx, w.Target.NodeUID)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(requestFor(id, w, intent, expires))
	if err != nil {
		return nil, admission.ErrUnavailable
	}
	pending := &binding{instance: instance, node: n, id: id, target: w.Target, profile: tracepreflight.Baseline().Digest(), expires: expires}
	response, err := n.call(ctx, http.MethodPost, "/v1/bindings", data, instance)
	if err != nil {
		if errors.Is(err, admission.ErrExpired) || errors.Is(err, admission.ErrCapacity) || errors.Is(err, admission.ErrTargetChanged) {
			return nil, err
		}
		return pending, err
	}
	defer response.Body.Close()
	var result bindResponse
	if decode(response.Body, &result, "cgroupID|profile") != nil || result.CgroupID == 0 || result.Profile != tracepreflight.Baseline().Digest() {
		return pending, admission.ErrUnavailable
	}
	target := w.Target
	target.CgroupID = result.CgroupID
	return &binding{instance: instance, node: n, id: id, target: target, profile: result.Profile, expires: expires}, nil
}

type binding struct {
	mu       sync.Mutex
	node     nodeClient
	id       string
	instance string
	target   trace.TargetIdentity
	profile  string
	expires  time.Time
	closed   bool
	claimed  bool
}

func (*binding) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "[private node binding]") }
func (*binding) MarshalJSON() ([]byte, error) {
	return nil, errors.New("node binding requires authorised transport")
}

func (b *binding) Target() trace.TargetIdentity { return b.target }
func (b *binding) ProfileDigest() string        { return b.profile }
func (b *binding) Revalidate(ctx context.Context) error {
	b.mu.Lock()
	expired := b.closed || !time.Now().Before(b.expires)
	b.mu.Unlock()
	if expired {
		return admission.ErrExpired
	}
	response, err := b.node.call(ctx, http.MethodGet, "/v1/bindings/"+b.id, nil, b.instance)
	if response != nil {
		response.Body.Close()
	}
	return err
}
func (b *binding) Close(ctx context.Context) error {
	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if closed {
		return nil
	}
	response, err := b.node.call(ctx, http.MethodDelete, "/v1/bindings/"+b.id, nil, b.instance)
	if response != nil {
		response.Body.Close()
	}
	if err == nil {
		b.mu.Lock()
		b.closed = true
		b.mu.Unlock()
	}
	return err
}
func (n nodeClient) call(ctx context.Context, method, path string, data []byte, instance string) (*http.Response, error) {
	// A non-replayable body disables transport retries for admission creation.
	var body io.Reader
	if data != nil {
		body = struct{ io.Reader }{bytes.NewReader(data)}
	}
	request, err := http.NewRequestWithContext(ctx, method, n.url+path, body)
	if err != nil {
		return nil, admission.ErrUnavailable
	}
	request.Header.Set(controllerHeader, n.owner)
	request.Header.Set(nodeHeader, instance)
	if data != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := n.http.Do(request)
	if err != nil {
		return nil, admission.ErrUnavailable
	}
	expected := http.StatusNoContent
	if method == http.MethodPost {
		expected = http.StatusOK
	}
	if response.StatusCode == expected {
		return response, nil
	}
	response.Body.Close()
	switch response.StatusCode {
	case 429:
		return nil, admission.ErrCapacity
	case 410:
		return nil, admission.ErrExpired
	case 409:
		return nil, admission.ErrTargetChanged
	default:
		return nil, admission.ErrUnavailable
	}
}
