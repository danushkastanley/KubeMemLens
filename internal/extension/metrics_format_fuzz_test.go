package extension

import (
	"strings"
	"testing"
)

// The Accept header comes from any authorised metrics reader. Negotiation
// must always choose one of the three representations, keep JSON for a
// missing header, and never let another media range outrank explicit JSON.
func FuzzNegotiateMetricsFormat(f *testing.F) {
	for _, seed := range []string{
		"", " ", "*/*", "application/json", "application/openmetrics-text; version=1.0.0",
		"text/plain; version=0.0.4; q=0.5, application/openmetrics-text; q=0.9", "text/plain;q=2",
		"application/openmetrics-text;version=9", ",,;;q=", "text/plain; q=0", "\x00\xff",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, accept string) {
		if len(accept) > 4096 {
			return
		}
		format := negotiateMetricsFormat(accept)
		if format != metricsJSON && format != metricsOpenMetrics && format != metricsPrometheusText {
			t.Fatalf("Accept %q negotiated an unknown format %v", accept, format)
		}
		if strings.TrimSpace(accept) == "" && format != metricsJSON {
			t.Fatalf("a missing Accept header negotiated %v", format)
		}
		if got := negotiateMetricsFormat(accept + ", application/json"); got != metricsJSON {
			t.Fatalf("Accept %q with explicit JSON added negotiated %v", accept, got)
		}
	})
}
