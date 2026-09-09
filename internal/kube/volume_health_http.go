package kube

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/rest"
)

const (
	maxHealthResponse   = 1 << 20
	maxHealthQueryBytes = 8 << 20
	maxHealthVolumes    = 64
)

type VolumeHealthOptions struct {
	Namespace string
	Timeout   time.Duration
	PodCache  *PodCache
}

type volumeHealthReader struct {
	client  *http.Client
	baseURL string
	opts    VolumeHealthOptions
}

// NewVolumeHealthSource uses only the supplied caller identity. A Pod informer
// is optional and never substitutes for live object-level API authorisation.
func NewVolumeHealthSource(config *rest.Config, opts VolumeHealthOptions) (volumehealth.SourceReader, error) {
	if config == nil || len(validation.IsDNS1123Label(opts.Namespace)) != 0 {
		return nil, errors.New("volume health requires Kubernetes configuration and one namespace")
	}
	base, err := url.Parse(config.Host)
	if err != nil || base.Host == "" || (base.Scheme != "https" && base.Scheme != "http") || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, errors.New("invalid Kubernetes API URL")
	}
	if opts.Timeout < 0 || opts.Timeout > time.Minute {
		return nil, errors.New("volume-health timeout must be at most one minute")
	}
	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Second
	}
	copy := rest.CopyConfig(config)
	copy.DisableCompression = true
	client, err := rest.HTTPClientFor(copy)
	if err != nil {
		return nil, errors.New("cannot create volume-health transport")
	}
	owned := &http.Client{Transport: client.Transport, Timeout: client.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &volumeHealthReader{client: owned, baseURL: strings.TrimRight(config.Host, "/"), opts: opts}, nil
}

type HealthReadError struct {
	Reason volumehealth.Reason
	cause  error
}

func (e *HealthReadError) Error() string { return "volume-health read failed: " + string(e.Reason) }
func (e *HealthReadError) Unwrap() error { return e.cause }

func invalidHealth() error { return &HealthReadError{Reason: volumehealth.InvalidResponse} }

type healthQuery struct {
	reader    *volumeHealthReader
	remaining int64
}

func (q *healthQuery) get(ctx context.Context, path string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, q.reader.baseURL+path, nil)
	if err != nil {
		return &HealthReadError{volumehealth.ReadFailed, err}
	}
	request.Header.Set("Accept", "application/json")
	response, err := q.reader.client.Do(request)
	if err != nil {
		return &HealthReadError{volumehealth.ReadFailed, err}
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnauthorized {
		return &HealthReadError{Reason: volumehealth.AccessDenied}
	}
	if response.StatusCode != http.StatusOK {
		return &HealthReadError{Reason: volumehealth.ReadFailed}
	}
	limit := min(int64(maxHealthResponse), q.remaining)
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return &HealthReadError{volumehealth.ReadFailed, err}
	}
	if int64(len(body)) > limit {
		return invalidHealth()
	}
	q.remaining -= int64(len(body))
	if err := json.Unmarshal(body, target); err != nil {
		return invalidHealth()
	}
	return nil
}

func healthFailure(err error) (volumehealth.Availability, volumehealth.Reason) {
	var readErr *HealthReadError
	if errors.As(err, &readErr) {
		if readErr.Reason == volumehealth.AccessDenied {
			return volumehealth.Forbidden, readErr.Reason
		}
		return volumehealth.Unavailable, readErr.Reason
	}
	return volumehealth.Unavailable, volumehealth.ReadFailed
}
