package resourcemetrics

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"k8s.io/client-go/rest"
)

func newClusterTestSource(t *testing.T, handler http.HandlerFunc, opts Options) Source {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	opts.Now = func() time.Time { return sampleTime }
	source, err := NewCluster(&rest.Config{Host: server.URL, BearerToken: "caller-token"}, opts)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func TestClusterPodMetricsKeepSameNamesInSeparateNamespaces(t *testing.T) {
	reads := 0
	source := newClusterTestSource(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apis/metrics.k8s.io":
			jsonResponse(t, w, groupBody("v1"))
		case "/apis/metrics.k8s.io/v1":
			jsonResponse(t, w, resourceBody("v1"))
		case "/apis/metrics.k8s.io/v1/pods":
			reads++
			if r.Header.Get("Authorization") != "Bearer caller-token" {
				t.Fatal("caller identity missing")
			}
			a := metricPod("app", sampleTime, map[string]string{"cpu": "1m", "memory": "1Mi"})
			b := metricPod("app", sampleTime, map[string]string{"cpu": "1m", "memory": "2Mi"}).(map[string]any)
			b["metadata"].(map[string]string)["namespace"] = "team-b"
			jsonResponse(t, w, listBody("v1", b, a))
		default:
			t.Errorf("unexpected scope %s", r.URL)
			w.WriteHeader(500)
		}
	}, Options{})
	report, err := source.Read(t.Context())
	if err != nil || report.Availability != Available || len(report.Observations) != 2 || reads != 1 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	if report.Observations[0].Identity.Namespace != "team-a" || report.Observations[1].Identity.Namespace != "team-b" {
		t.Fatal("unstable ordering or lost namespace")
	}
}

func TestClusterScopeIsExplicitAndDenialDoesNotEnumerateNamespaces(t *testing.T) {
	if _, err := New(&rest.Config{Host: "https://example.invalid"}, Options{}); err == nil {
		t.Fatal("omitted namespace broadened a read")
	}
	if _, err := NewCluster(&rest.Config{Host: "https://example.invalid"}, Options{Namespace: "team-a"}); err == nil {
		t.Fatal("ambiguous cluster scope accepted")
	}
	source := newClusterTestSource(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apis/metrics.k8s.io":
			jsonResponse(t, w, groupBody("v1"))
		case "/apis/metrics.k8s.io/v1":
			jsonResponse(t, w, resourceBody("v1"))
		case "/apis/metrics.k8s.io/v1/pods":
			w.WriteHeader(403)
		default:
			t.Errorf("unexpected fallback %s", r.URL)
			w.WriteHeader(500)
		}
	}, Options{})
	report, err := source.Read(t.Context())
	if err != nil || report.Availability != Forbidden || len(report.Observations) != 0 {
		t.Fatalf("%+v %v", report, err)
	}
}

func TestMemorySurvivesMissingCPUAndMeasuredZeroStaysKnown(t *testing.T) {
	source := newTestSource(t, apiHandler(t, []string{"v1"}, func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(t, w, listBody("v1", metricPod("memory-only", sampleTime, map[string]string{"memory": "1Ki"}), metricPod("zero", sampleTime, map[string]string{"memory": "0", "cpu": "0"})))
	}), Options{})
	report, err := source.Read(t.Context())
	if err != nil || report.Availability != Partial || report.OmittedContainers != 0 || len(report.Observations) != 2 {
		t.Fatalf("%+v %v", report, err)
	}
	if report.Observations[0].CPUUsageKnown || report.Observations[0].MemoryWorkingSetBytes != 1024 {
		t.Fatal("valid memory was lost or CPU was fabricated")
	}
	if !report.Observations[1].CPUUsageKnown || report.Observations[1].CPUUsageNanocores != 0 || report.Observations[1].MemoryWorkingSetBytes != 0 {
		t.Fatal("measured zero lost")
	}
}
