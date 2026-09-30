package traceclient

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/client-go/rest"
)

const apiPrefix = "/apis/tracing.kubememlens.io/v1alpha1"
const controlTimeout = 5 * time.Second
const maxControlBytes = 32 << 10

type Client struct {
	base        string
	http        *http.Client
	stream      *http.Client
	streamSlot  chan struct{}
	controlSlot chan struct{}
	cancelSlot  chan struct{}
}

func New(config *rest.Config) (*Client, error) {
	if config == nil {
		return nil, failure(Configuration)
	}
	u, err := url.Parse(config.Host)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || config.Insecure || config.Transport != nil || config.WrapTransport != nil {
		return nil, failure(Configuration)
	}
	cfg := rest.CopyConfig(config)
	cfg.Timeout = 0 // Per-operation contexts distinguish control from bounded streams.
	cfg.DisableCompression = true
	// A non-nil proxy function gives this client an unshared native transport.
	// Preserve client-go's certificate/CA reload and credential wrappers intact.
	if cfg.Proxy == nil {
		cfg.Proxy = http.ProxyFromEnvironment
	}
	h, err := rest.HTTPClientFor(cfg)
	if err != nil {
		return nil, failure(Configuration)
	}
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	cfg.NextProtos = []string{"http/1.1"}
	stream, err := rest.HTTPClientFor(cfg)
	if err != nil {
		utilnet.CloseIdleConnectionsFor(h.Transport)
		return nil, failure(Configuration)
	}
	stream.CheckRedirect = h.CheckRedirect
	return &Client{base: strings.TrimRight(config.Host, "/"), http: h, stream: stream, streamSlot: make(chan struct{}, 1), controlSlot: make(chan struct{}, 1), cancelSlot: make(chan struct{}, 1)}, nil
}
func (c *Client) Close() {
	if c != nil && c.http != nil {
		utilnet.CloseIdleConnectionsFor(c.http.Transport)
		utilnet.CloseIdleConnectionsFor(c.stream.Transport)
	}
}

func (c *Client) open(ctx context.Context, method, path string, data []byte) (*http.Response, error) {
	if c == nil || c.http == nil {
		return nil, failure(Invalid)
	}
	var body io.Reader
	if data != nil {
		body = struct{ io.Reader }{bytes.NewReader(data)}
	} // No replayable mutation body.
	r, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, failure(Invalid)
	}
	r.Header.Set("Accept", "application/json")
	r.Header.Set("Cache-Control", "no-store")
	if data != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(r)
	if err != nil {
		return nil, failure(Unavailable)
	}
	return response, nil
}

func (c *Client) control(ctx context.Context, method, path string, data []byte, expected, maximum int) ([]byte, int, error) {
	if c == nil {
		return nil, 0, failure(Invalid)
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	slot := c.controlSlot
	if method == http.MethodDelete {
		slot = c.cancelSlot
	}
	select {
	case slot <- struct{}{}:
		defer func() { <-slot }()
	default:
		return nil, 0, failure(Capacity)
	}
	ctx, cancel := context.WithTimeout(ctx, controlTimeout)
	defer cancel()
	r, err := c.open(ctx, method, path, data)
	if err != nil {
		return nil, 0, err
	}
	defer r.Body.Close()
	if !boundedHeaders(r.Header) {
		return nil, r.StatusCode, failure(Protocol)
	}
	if r.StatusCode != expected {
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 4096))
		return nil, r.StatusCode, statusError(r.StatusCode)
	}
	if r.ContentLength > int64(maximum) {
		return nil, r.StatusCode, failure(Protocol)
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(maximum)+1))
	if err != nil || len(body) > maximum {
		return nil, r.StatusCode, failure(Protocol)
	}
	return body, r.StatusCode, nil
}

func boundedHeaders(header http.Header) bool {
	size := 2
	for name, values := range header {
		for _, value := range values {
			size += len(name) + len(value) + 4
			if size > 16<<10 {
				return false
			}
		}
	}
	return true
}
func statusError(status int) error {
	switch status {
	case 400, 413, 422:
		return failure(Invalid)
	case 401, 403:
		return failure(Denied)
	case 404, 410:
		return failure(Gone)
	case 409:
		return failure(TargetChanged)
	case 429:
		return failure(Capacity)
	case 502, 503, 504:
		return failure(Unavailable)
	default:
		return failure(Protocol)
	}
}
func decode(data []byte, out any) error {
	if json.Unmarshal(data, out, json.RejectUnknownMembers(true)) != nil {
		return failure(Protocol)
	}
	return nil
}
