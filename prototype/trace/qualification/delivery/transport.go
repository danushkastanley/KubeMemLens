package delivery

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/qualificationendpoint"
)

type Connection struct {
	Server, Token, CAPEM string
	NetworkScope         qualificationendpoint.Scope
}

func (Connection) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[private delivery connection]")
}
func (Connection) MarshalJSON() ([]byte, error) { return nil, ErrObservation }

// Connect attaches to an existing admission. GET /stream activates its worker;
// the controller must cancel that owned admission on any failure and verify cleanup.
func Connect(ctx context.Context, connection Connection, expected Expectation) (Result, error) {
	if !validExpectation(expected) {
		return Result{}, ErrObservation
	}
	client, err := newHTTPClient(connection)
	if err != nil {
		return Result{}, err
	}
	defer client.CloseIdleConnections()
	lifetime, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	return readWithClient(lifetime, client, connection.Server, connection.Token, expected, time.Now)
}

func newHTTPClient(connection Connection) (*http.Client, error) {
	if !qualificationendpoint.Allowed(connection.Server, connection.NetworkScope) {
		return nil, ErrObservation
	}
	if connection.Token == "" || len(connection.Token) > 8192 || strings.ContainsAny(connection.Token, " \t\r\n") || len(connection.CAPEM) > 16384 {
		return nil, ErrObservation
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(connection.CAPEM)) {
		return nil, ErrObservation
	}
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second, MaxResponseHeaderBytes: 16384, MaxConnsPerHost: 2, DisableCompression: true}
	client := &http.Client{Transport: transport, Timeout: 40 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return client, nil
}

func readWithClient(ctx context.Context, client *http.Client, server, token string, expected Expectation, now func() time.Time) (Result, error) {
	body, active, err := openAdmittedStream(ctx, client, server, token, expected)
	if err != nil {
		return Result{}, err
	}
	defer body.Close()
	return Observe(body, expected, now, active)
}

func openAdmittedStream(ctx context.Context, client *http.Client, server, token string, expected Expectation) (io.ReadCloser, func() (time.Time, error), error) {
	target := expected.Specification.Target()
	path := "/apis/tracing.kubememlens.io/v1alpha1/namespaces/" + url.PathEscape(target.Namespace) + "/traces/" + url.PathEscape(expected.SessionID)
	get := func(suffix string) (*http.Response, error) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, server+path+suffix, nil)
		if err != nil {
			return nil, ErrObservation
		}
		request.Header.Set("Authorization", "Bearer "+token)
		response, err := client.Do(request)
		if err != nil {
			return nil, ErrObservation
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return nil, ErrObservation
		}
		return response, nil
	}
	response, err := get("/stream")
	if err != nil {
		return nil, nil, ErrObservation
	}
	active := func() (time.Time, error) {
		state, err := get("")
		if err != nil {
			return time.Time{}, ErrObservation
		}
		defer state.Body.Close()
		data, err := io.ReadAll(io.LimitReader(state.Body, 65537))
		if err != nil || len(data) > 65536 {
			return time.Time{}, ErrObservation
		}
		var admission struct {
			APIVersion string `json:"apiVersion"`
			Kind       string `json:"kind"`
			Metadata   struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
			State        string    `json:"state"`
			ExpiresAt    time.Time `json:"expiresAt"`
			EngineDigest string    `json:"engineDigest"`
		}
		if json.Unmarshal(data, &admission) != nil || admission.APIVersion != "tracing.kubememlens.io/v1alpha1" || admission.Kind != "TraceAdmission" || admission.Metadata.Name != expected.SessionID || admission.Metadata.Namespace != target.Namespace || admission.State != "active" || admission.EngineDigest != expected.EngineDigest || admission.ExpiresAt.IsZero() {
			return time.Time{}, ErrObservation
		}
		return admission.ExpiresAt, nil
	}
	return response.Body, active, nil
}
