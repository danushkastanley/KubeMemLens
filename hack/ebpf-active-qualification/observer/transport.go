package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var kindHost = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,100}-control-plane$`)

func collectorClient(server, token, ca string) (*http.Client, error) {
	endpoint, err := url.Parse(server)
	if err != nil || endpoint.Scheme != "https" || endpoint.User != nil || endpoint.Path != "" || endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" || endpoint.String() != server || endpoint.Port() == "" {
		return nil, errObservation
	}
	host := endpoint.Hostname()
	ip := net.ParseIP(host)
	if !(ip != nil && ip.IsLoopback()) && !kindHost.MatchString(host) && !(host == "kubernetes.default.svc" && endpoint.Port() == "443") {
		return nil, errObservation
	}
	if token == "" || len(token) > 8192 || strings.ContainsAny(token, " \t\r\n") || len(ca) > 16384 {
		return nil, errObservation
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(ca)) {
		return nil, errObservation
	}
	tr := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}, DialContext: (&net.Dialer{Timeout: time.Second}).DialContext, TLSHandshakeTimeout: time.Second, ResponseHeaderTimeout: time.Second, MaxResponseHeaderBytes: 16384, MaxConnsPerHost: 1, MaxIdleConnsPerHost: 1, IdleConnTimeout: 5 * time.Second, DisableCompression: true}
	return &http.Client{Transport: tr, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}

func readCollector(ctx context.Context, client *http.Client, server, token string) (collectorObservation, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server+"/apis/memory.kubememlens.io/v1alpha1/metrics/current", nil)
	if err != nil {
		return collectorObservation{}, atStage("collector-client", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(req)
	if err != nil {
		return collectorObservation{}, atStage("collector-transport", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return collectorObservation{}, atStage("collector-status", errObservation)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil || len(data) > 2<<20 {
		return collectorObservation{}, atStage("collector-body", errObservation)
	}
	var envelope struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Metadata   struct {
			Name              string          `json:"name"`
			Namespace         string          `json:"namespace"`
			CreationTimestamp json.RawMessage `json:"creationTimestamp"`
		} `json:"metadata"`
		ContentType string `json:"contentType"`
		Content     string `json:"content"`
	}
	if unambiguousJSON(data) != nil {
		return collectorObservation{}, atStage("collector-envelope", errObservation)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if dec.Decode(&envelope) != nil || dec.Decode(new(any)) != io.EOF || envelope.APIVersion != "memory.kubememlens.io/v1alpha1" || envelope.Kind != "Metrics" || envelope.Metadata.Name != "current" || envelope.Metadata.Namespace != "" || envelope.ContentType != "application/openmetrics-text; version=1.0.0; charset=utf-8" {
		return collectorObservation{}, atStage("collector-envelope", errObservation)
	}
	value, err := parseCollector([]byte(envelope.Content))
	if err != nil {
		return collectorObservation{}, atStage("collector-metrics", err)
	}
	return value, nil
}

func scrapeAgent(ctx context.Context, output io.Writer) error {
	tr := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: time.Second}).DialContext, ResponseHeaderTimeout: time.Second, MaxResponseHeaderBytes: 16384, DisableCompression: true}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:8082/metrics", nil)
	if err != nil {
		return atStage("agent-transport", err)
	}
	response, err := client.Do(req)
	if err != nil {
		return atStage("agent-transport", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return atStage("agent-status", errObservation)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return atStage("agent-body", errObservation)
	}
	value, err := parseAgent(data)
	if err != nil {
		return atStage("agent-metrics", err)
	}
	if err := json.NewEncoder(output).Encode(value); err != nil {
		return atStage("agent-projection", err)
	}
	return nil
}
