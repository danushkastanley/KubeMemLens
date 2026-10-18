// Package promhistory implements the fixed, identity-bound Prometheus history
// profile. It never accepts PromQL, credentials or endpoint URLs from readers.
package promhistory

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

type Options struct {
	URL         string
	Cluster     string
	CAData      []byte
	BearerToken string
}

type Client struct {
	client   *http.Client
	endpoint string
	cluster  string
	token    string
	gate     chan struct{}
	now      func() time.Time
}

func New(opts Options) (*Client, error) {
	u, err := url.Parse(opts.URL)
	if err != nil || len(opts.URL) > 2048 || u.Scheme != "https" || u.Hostname() == "" || u.ForceQuery || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || strings.Contains(u.Path, "..") {
		return nil, memoryhistory.ErrInvalid
	}
	if len(opts.Cluster) == 0 || len(opts.Cluster) > 128 || strings.IndexFunc(opts.Cluster, func(r rune) bool { return r < 33 || r > 126 }) >= 0 {
		return nil, memoryhistory.ErrInvalid
	}
	if len(opts.BearerToken) > 16384 || strings.IndexFunc(opts.BearerToken, func(r rune) bool { return r <= 32 || r >= 127 }) >= 0 {
		return nil, memoryhistory.ErrInvalid
	}
	var roots *x509.CertPool
	if len(opts.CAData) > 0 {
		if len(opts.CAData) > 1<<20 {
			return nil, memoryhistory.ErrBounds
		}
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM(opts.CAData) {
			return nil, memoryhistory.ErrInvalid
		}
	}
	transport := &http.Transport{
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots},
		DialContext:         (&net.Dialer{Timeout: 2 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout: 2 * time.Second, ResponseHeaderTimeout: 3 * time.Second,
		MaxResponseHeaderBytes: 16 << 10, DisableCompression: true,
		MaxIdleConns: 1, MaxIdleConnsPerHost: 1, MaxConnsPerHost: 1, IdleConnTimeout: 30 * time.Second,
	}
	client := &http.Client{Transport: transport, Timeout: memoryhistory.QueryTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &Client{client: client, endpoint: strings.TrimRight(u.String(), "/") + "/api/v1/query_range", cluster: opts.Cluster, token: opts.BearerToken, gate: make(chan struct{}, 1), now: func() time.Time { return time.Now().UTC() }}, nil
}

func (c *Client) Close() { c.client.CloseIdleConnections() }

func (c *Client) fetch(ctx context.Context, values url.Values) ([]byte, error) {
	body := values.Encode()
	if len(body) > 32<<10 {
		return nil, memoryhistory.ErrBounds
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, strings.NewReader(body))
	if err != nil {
		return nil, memoryhistory.ErrInvalid
	}
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Accept", "application/json")
	r.Header.Set("Accept-Encoding", "identity")
	if c.token != "" {
		r.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.client.Do(r)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, memoryhistory.ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, memoryhistory.ErrUnavailable
	}
	media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	encoding := response.Header.Get("Content-Encoding")
	if err != nil || media != "application/json" || (encoding != "" && encoding != "identity") {
		return nil, memoryhistory.ErrSource
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, memoryhistory.MaxResponseBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, memoryhistory.ErrUnavailable
	}
	if len(data) > memoryhistory.MaxResponseBytes {
		return nil, memoryhistory.ErrSource
	}
	return data, nil
}
