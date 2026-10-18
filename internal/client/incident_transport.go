package client

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"github.com/danushkastanley/kube-memlens/internal/kube"
	"k8s.io/client-go/rest"
)

type IncidentClient struct {
	api       *KubernetesAPIClient
	namespace string
}

// IncidentError never formats request bodies, resource names or transport URLs.
// Mutations are not retried automatically, including uncertain outcomes.
type IncidentError struct {
	StatusCode     int
	OutcomeUnknown bool
}

func (e *IncidentError) Error() string {
	if e.OutcomeUnknown {
		return "incident action outcome could not be confirmed; it was not retried"
	}
	switch e.StatusCode {
	case 400:
		return "invalid incident session request"
	case 401, 403:
		return "incident session access denied"
	case 404:
		return "incident session or evidence is unavailable"
	case 409:
		return "incident session is closed"
	case 429:
		return "incident session capacity reached"
	default:
		return "incident session service or response is unavailable"
	}
}
func (e *IncidentError) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, e.Error()) }

func NewIncidentSessionClient(config *rest.Config, scope ReadScope) (*IncidentClient, error) {
	if config == nil || config.Insecure || scope.AllNamespaces || scope.validate() != nil {
		return nil, incidentsession.ErrInvalid
	}
	if !verifiedIncidentTLS(config) {
		return nil, incidentsession.ErrInvalid
	}
	copied := rest.CopyConfig(config)
	copied.DisableCompression = true
	api, err := NewKubernetesAPIClient(copied, scope, 12*time.Second)
	if err != nil {
		return nil, incidentsession.ErrUnavailable
	}
	api.httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &IncidentClient{api: api, namespace: scope.Namespace}, nil
}

func NewIncidentSessionConnection(options Options) (*IncidentClient, error) {
	opts, err := options.WithDefaults()
	if err != nil {
		return nil, incidentsession.ErrInvalid
	}
	mode, err := ResolveMode(opts)
	if err != nil || mode != ConnectionModeKubernetesAPI || opts.EvidenceMode == capability.Restricted {
		return nil, errors.New("incident sessions require the authenticated Kubernetes API connection")
	}
	config, err := kube.BuildConfig(opts.Kubeconfig, opts.Context)
	if err != nil {
		return nil, incidentsession.ErrUnavailable
	}
	return NewIncidentSessionClient(config, opts.ReadScope)
}

func (c *IncidentClient) Close() { c.api.httpClient.CloseIdleConnections() }

func validIncidentID(id string) bool {
	if len(id) != 32 || id != strings.ToLower(id) {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func (c *IncidentClient) exchange(ctx context.Context, method, id, action string, body []byte, expected, limit int) ([]byte, error) {
	if ctx == nil || ctx.Err() != nil {
		if ctx != nil {
			return nil, ctx.Err()
		}
		return nil, incidentsession.ErrInvalid
	}
	if (id != "" && !validIncidentID(id)) || len(body) > 4096 {
		return nil, incidentsession.ErrInvalid
	}
	path := c.api.baseURL + "/namespaces/" + c.namespace + "/incidentsessions"
	if id != "" {
		path += "/" + id
	}
	if action != "" {
		path += "/" + action
	}
	request, err := http.NewRequestWithContext(ctx, method, path, bytes.NewReader(body))
	if err != nil {
		return nil, incidentsession.ErrInvalid
	}
	request.Header.Set(incidentsession.SchemaHeader, "1")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Accept-Encoding", "identity")
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.api.httpClient.Do(request)
	mutation := method != http.MethodGet
	if err != nil {
		return nil, &IncidentError{OutcomeUnknown: mutation}
	}
	defer response.Body.Close()
	if response.StatusCode != expected {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, &IncidentError{StatusCode: response.StatusCode, OutcomeUnknown: mutation && (response.StatusCode >= 500 || (response.StatusCode < 400))}
	}
	failure := &IncidentError{OutcomeUnknown: mutation}
	values := response.Header.Values(incidentsession.SchemaHeader)
	if len(values) != 1 || values[0] != "1" || response.ContentLength > int64(limit) || response.Header.Get("Content-Encoding") != "" || response.Uncompressed {
		return nil, failure
	}
	if expected != http.StatusNoContent {
		media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
		if err != nil || media != "application/json" {
			return nil, failure
		}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, int64(limit)+1))
	if err != nil || len(data) > limit {
		return nil, failure
	}
	return data, nil
}
