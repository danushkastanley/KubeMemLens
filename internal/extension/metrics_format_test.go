package extension

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/collector"
	"github.com/danushkastanley/kube-memlens/internal/metrics"
)

const (
	metricsPath = "/apis/memory.kubememlens.io/v1alpha1/metrics/current"
	// Default scrape Accept headers sent by Prometheus 3 and Prometheus 2.
	prometheus3Accept = "application/openmetrics-text;version=1.0.0;escaping=allow-utf-8;q=0.5,application/openmetrics-text;version=0.0.1;q=0.4,text/plain;version=1.0.0;escaping=allow-utf-8;q=0.3,text/plain;version=0.0.4;q=0.2,*/*;q=0.1"
	prometheus2Accept = "application/openmetrics-text;version=1.0.0,application/openmetrics-text;version=0.0.1;q=0.75,text/plain;version=0.0.4;q=0.5,*/*;q=0.1"
	kubectlRawAccept  = "application/json, */*"
)

func TestNegotiateMetricsFormat(t *testing.T) {
	cases := []struct {
		name   string
		accept string
		want   metricsFormat
	}{
		{name: "missing header keeps JSON", accept: "", want: metricsJSON},
		{name: "kubectl get --raw keeps JSON", accept: kubectlRawAccept, want: metricsJSON},
		{name: "wildcard keeps JSON", accept: "*/*", want: metricsJSON},
		{name: "explicit JSON", accept: "application/json", want: metricsJSON},
		{name: "Prometheus 3 scrape", accept: prometheus3Accept, want: metricsOpenMetrics},
		{name: "Prometheus 2 scrape", accept: prometheus2Accept, want: metricsOpenMetrics},
		{name: "OpenMetrics without version", accept: "application/openmetrics-text", want: metricsOpenMetrics},
		{name: "classic text scraper", accept: "text/plain;version=0.0.4", want: metricsPrometheusText},
		{name: "text without version", accept: "text/plain", want: metricsPrometheusText},
		{name: "text preferred over low-quality JSON", accept: "application/json;q=0.1, text/plain", want: metricsPrometheusText},
		{name: "explicit text beats wildcard at equal quality", accept: "text/plain, */*", want: metricsPrometheusText},
		{name: "JSON wins a tie with OpenMetrics", accept: "application/json, application/openmetrics-text", want: metricsJSON},
		{name: "OpenMetrics wins a tie with text", accept: "text/plain, application/openmetrics-text", want: metricsOpenMetrics},
		{name: "unsupported OpenMetrics version ignored", accept: "application/openmetrics-text;version=2.0.0", want: metricsJSON},
		{name: "unsupported text version ignored", accept: "text/plain;version=1.0.0", want: metricsJSON},
		{name: "zero quality is refused", accept: "text/plain;q=0", want: metricsJSON},
		{name: "malformed quality ignored", accept: "text/plain;q=high", want: metricsJSON},
		{name: "out-of-range quality ignored", accept: "text/plain;q=2", want: metricsJSON},
		{name: "unknown media type keeps JSON", accept: "image/png", want: metricsJSON},
		{name: "malformed header keeps JSON", accept: ";;;", want: metricsJSON},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := negotiateMetricsFormat(tc.accept); got != tc.want {
				t.Fatalf("negotiateMetricsFormat(%q) = %d, want %d", tc.accept, got, tc.want)
			}
		})
	}
}

func serveMetricsWithAccept(t *testing.T, h http.Handler, accept string, authenticated bool) *httptest.ResponseRecorder {
	t.Helper()
	request := readRequest(t, metricsPath, authenticated)
	if accept != "" {
		request.Header.Set("Accept", accept)
	}
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	return recorder
}

func TestMetricsServeExpositionTextToScrapers(t *testing.T) {
	h := NewReadHandler(collector.NewStore(), collector.DefaultHandlerOptions(time.Minute))
	for _, tc := range []struct {
		accept      string
		contentType string
	}{
		{accept: prometheus3Accept, contentType: metrics.ContentType},
		{accept: "text/plain;version=0.0.4", contentType: prometheusTextContentType},
	} {
		w := serveMetricsWithAccept(t, h, tc.accept, true)
		if w.Code != http.StatusOK {
			t.Fatalf("Accept %q: status %d: %s", tc.accept, w.Code, w.Body.String())
		}
		if got := w.Header().Get("Content-Type"); got != tc.contentType {
			t.Fatalf("Accept %q: Content-Type = %q, want %q", tc.accept, got, tc.contentType)
		}
		if w.Header().Get("Vary") != "Accept" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("Accept %q: missing Vary or nosniff headers: %v", tc.accept, w.Header())
		}
		body := w.Body.String()
		if strings.HasPrefix(body, "{") || !strings.Contains(body, "\nkubememlens_collector_heap_objects_bytes ") || !strings.HasSuffix(body, "# EOF\n") {
			t.Fatalf("Accept %q: body is not exposition text: %.200q", tc.accept, body)
		}
	}
}

func TestMetricsDefaultResponseRemainsJSONEnvelope(t *testing.T) {
	h := NewReadHandler(collector.NewStore(), collector.DefaultHandlerOptions(time.Minute))
	for _, accept := range []string{"", kubectlRawAccept} {
		w := serveMetricsWithAccept(t, h, accept, true)
		if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/json" || w.Header().Get("Vary") != "Accept" {
			t.Fatalf("Accept %q: status %d, headers %v", accept, w.Code, w.Header())
		}
		var value api.Metrics
		if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil || value.Kind != "Metrics" || value.ContentType != metrics.ContentType || !strings.HasSuffix(value.Content, "# EOF\n") {
			t.Fatalf("Accept %q: envelope = %+v, err = %v", accept, value, err)
		}
	}
}

func TestMetricsTextKeepsAuthenticationAndErrorContracts(t *testing.T) {
	opts := collector.DefaultHandlerOptions(time.Minute)
	h := NewReadHandler(collector.NewStore(), opts)

	unauthenticated := serveMetricsWithAccept(t, h, prometheus3Accept, false)
	if unauthenticated.Code != http.StatusUnauthorized || strings.Contains(unauthenticated.Body.String(), "kubememlens_") {
		t.Fatalf("unauthenticated scrape: status %d body %q", unauthenticated.Code, unauthenticated.Body.String())
	}

	h.opts.Metrics.Enabled = false
	disabled := serveMetricsWithAccept(t, h, prometheus3Accept, true)
	if disabled.Code != http.StatusNotFound || disabled.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("disabled metrics: status %d headers %v", disabled.Code, disabled.Header())
	}

	h.opts.Metrics.Enabled = true
	h.opts.MaxResponseBytes = 64
	tooLarge := serveMetricsWithAccept(t, h, prometheus3Accept, true)
	if tooLarge.Code != http.StatusInsufficientStorage || strings.Contains(tooLarge.Body.String(), "kubememlens_") {
		t.Fatalf("oversized scrape: status %d body %.200q", tooLarge.Code, tooLarge.Body.String())
	}
}
