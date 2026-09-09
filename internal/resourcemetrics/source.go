package resourcemetrics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/rest"
)

const group = "metrics.k8s.io"

type kubernetesSource struct {
	client  *http.Client
	baseURL string
	opts    Options
}

func New(config *rest.Config, options Options) (Source, error) {
	if config == nil {
		return nil, fmt.Errorf("Kubernetes configuration is required")
	}
	if len(validation.IsDNS1123Label(options.Namespace)) != 0 {
		return nil, fmt.Errorf("one valid Kubernetes namespace is required")
	}
	base, err := url.Parse(config.Host)
	if err != nil || base.Host == "" || (base.Scheme != "https" && base.Scheme != "http") || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, fmt.Errorf("invalid Kubernetes API URL")
	}
	if err := validateOptions(options); err != nil {
		return nil, err
	}
	opts := defaultOptions(options)
	copied := rest.CopyConfig(config)
	copied.DisableCompression = true
	client, err := rest.HTTPClientFor(copied)
	if err != nil {
		return nil, fmt.Errorf("create resource-metrics client: %w", err)
	}
	ownedClient := &http.Client{Transport: client.Transport, Timeout: client.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &kubernetesSource{client: ownedClient, baseURL: strings.TrimRight(config.Host, "/"), opts: opts}, nil
}

func defaultOptions(opts Options) Options {
	if opts.Timeout <= 0 {
		opts.Timeout = 5 * time.Second
	}
	if opts.MaxAge <= 0 {
		opts.MaxAge = 2 * time.Minute
	}
	if opts.MaxFutureSkew <= 0 {
		opts.MaxFutureSkew = 5 * time.Second
	}
	if opts.MaxResponseBytes <= 0 {
		opts.MaxResponseBytes = 4 << 20
	}
	if opts.PageSize <= 0 {
		opts.PageSize = 500
	}
	if opts.MaxPages <= 0 {
		opts.MaxPages = 4
	}
	if opts.MaxPods <= 0 {
		opts.MaxPods = 2000
	}
	if opts.MaxContainers <= 0 {
		opts.MaxContainers = 10000
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return opts
}

func (s *kubernetesSource) Read(ctx context.Context) (Report, error) {
	ctx, cancel := context.WithTimeout(ctx, s.opts.Timeout)
	defer cancel()
	version, failure, err := s.discover(ctx)
	if failure != nil {
		return *failure, err
	}
	return s.readPods(ctx, version)
}

type requestError struct {
	reason Reason
	cause  error
}

func (e *requestError) Error() string { return "resource-metrics read failed: " + string(e.reason) }
func (e *requestError) Unwrap() error { return e.cause }

func (s *kubernetesSource) get(ctx context.Context, path string, query url.Values, target any) (int, error) {
	endpoint := s.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, &requestError{RequestFailed, err}
	}
	request.Header.Set("Accept", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return 0, &requestError{RequestFailed, err}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return response.StatusCode, nil
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, s.opts.MaxResponseBytes+1))
	if err != nil {
		return response.StatusCode, &requestError{RequestFailed, err}
	}
	if int64(len(body)) > s.opts.MaxResponseBytes {
		return response.StatusCode, &requestError{InvalidResponse, errors.New("response size limit exceeded")}
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(target); err != nil {
		return response.StatusCode, &requestError{InvalidResponse, err}
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return response.StatusCode, &requestError{InvalidResponse, errors.New("trailing response data")}
	}
	return response.StatusCode, nil
}

func failureReport(status int, err error, discovery bool) Report {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return Report{Availability: Forbidden, Reason: AccessDenied}
	case discovery && status == http.StatusNotFound:
		return Report{Availability: MissingProvider, Reason: DiscoveryMissing}
	default:
		reason := RequestFailed
		var requestErr *requestError
		if errors.As(err, &requestErr) {
			reason = requestErr.reason
		}
		return Report{Availability: Unavailable, Reason: reason}
	}
}
