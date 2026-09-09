package resourcemetrics

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/rest"
)

var sampleTime = time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)

func groupBody(versions ...string) any {
	entries := []map[string]string{}
	for _, version := range versions {
		entries = append(entries, map[string]string{"version": version, "groupVersion": group + "/" + version})
	}
	return map[string]any{"name": group, "versions": entries}
}
func resourceBody(version string) any {
	return map[string]any{"groupVersion": group + "/" + version, "resources": []any{map[string]any{"name": "pods", "namespaced": true, "verbs": []string{"get", "list"}}}}
}
func metricPod(name string, at time.Time, usage map[string]string) any {
	return map[string]any{"metadata": map[string]string{"namespace": "team-a", "name": name, "uid": "uid-" + name}, "timestamp": at.Format(time.RFC3339Nano), "window": "15s", "containers": []any{map[string]any{"name": "worker", "usage": usage}}}
}
func listBody(version string, pods ...any) any {
	return map[string]any{"apiVersion": group + "/" + version, "kind": "PodMetricsList", "items": pods}
}
func jsonResponse(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Error(err)
	}
}
func newTestSource(t *testing.T, handler http.HandlerFunc, options Options) Source {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	if options.Namespace == "" {
		options.Namespace = "team-a"
	}
	options.Now = func() time.Time { return sampleTime }
	source, err := New(&rest.Config{Host: server.URL, BearerToken: "caller-token"}, options)
	if err != nil {
		t.Fatal(err)
	}
	return source
}
func apiHandler(t *testing.T, versions []string, list http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer caller-token" {
			t.Error("caller identity or read-only method changed")
		}
		if r.URL.Path == "/apis/"+group {
			jsonResponse(t, w, groupBody(versions...))
			return
		}
		for _, version := range versions {
			if r.URL.Path == "/apis/"+group+"/"+version {
				jsonResponse(t, w, resourceBody(version))
				return
			}
		}
		if !strings.Contains(r.URL.Path, "/namespaces/team-a/pods") {
			t.Error("namespace scope changed")
		}
		list(w, r)
	}
}

func TestDiscoveryPrefersV1AndSupportsTransition(t *testing.T) {
	for _, tc := range []struct {
		name     string
		versions []string
		want     string
	}{{"both", []string{"v1beta1", "v1"}, "v1"}, {"transition", []string{"v1beta1"}, "v1beta1"}} {
		t.Run(tc.name, func(t *testing.T) {
			source := newTestSource(t, apiHandler(t, tc.versions, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/apis/"+group+"/"+tc.want+"/namespaces/team-a/pods" || r.URL.Query().Get("limit") != "500" {
					t.Error(r.URL)
				}
				jsonResponse(t, w, listBody(tc.want, metricPod("app", sampleTime, map[string]string{"cpu": "250m", "memory": "64Mi"})))
			}), Options{})
			report, err := source.Read(t.Context())
			if err != nil || report.Availability != Available || len(report.Observations) != 1 {
				t.Fatalf("%+v %v", report, err)
			}
			got := report.Observations[0]
			if got.APIVersion != group+"/"+tc.want || got.Identity.PodUID != "uid-app" || got.Identity.Namespace != "team-a" || got.CPUUsageNanocores != 250000000 || got.MemoryWorkingSetBytes != 64<<20 || got.Timestamp != sampleTime || got.Window != 15*time.Second || got.Freshness != Fresh {
				t.Fatal(got)
			}
			if report.ProviderCoverageKnown {
				t.Fatal("provider completeness was invented")
			}
		})
	}
}

func TestMissingProviderAndDeniedDiscoveryRemainDistinct(t *testing.T) {
	for status, want := range map[int]Availability{404: MissingProvider, 403: Forbidden, 401: Forbidden, 503: Unavailable} {
		source := newTestSource(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/apis/"+group {
				t.Error("unexpected fallback read")
			}
			w.WriteHeader(status)
		}, Options{})
		report, err := source.Read(t.Context())
		if err != nil || report.Availability != want || len(report.Observations) != 0 {
			t.Fatalf("status %d: %+v %v", status, report, err)
		}
	}
}

func TestServedV1FailureNeverFallsBackToBeta(t *testing.T) {
	for _, status := range []int{403, 404, 500} {
		reads := 0
		source := newTestSource(t, apiHandler(t, []string{"v1", "v1beta1"}, func(w http.ResponseWriter, r *http.Request) {
			reads++
			if strings.Contains(r.URL.Path, "v1beta1") {
				t.Error("permission or provider failure triggered fallback")
			}
			w.WriteHeader(status)
		}), Options{})
		report, err := source.Read(t.Context())
		if err != nil || reads != 1 || len(report.Observations) != 0 || report.APIVersion != group+"/v1" {
			t.Fatalf("%+v %v reads=%d", report, err, reads)
		}
	}
}

func TestMissingUsageIsOmittedAndRealZeroIsRetained(t *testing.T) {
	source := newTestSource(t, apiHandler(t, []string{"v1"}, func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(t, w, listBody("v1", metricPod("missing", sampleTime, map[string]string{"cpu": "1m"}), metricPod("zero", sampleTime, map[string]string{"cpu": "0", "memory": "0"})))
	}), Options{})
	report, err := source.Read(t.Context())
	if err != nil || report.Availability != Partial || report.OmittedContainers != 1 || len(report.Observations) != 1 || report.Observations[0].Identity.PodName != "zero" || report.Observations[0].MemoryWorkingSetBytes != 0 {
		t.Fatalf("%+v %v", report, err)
	}
}

func TestFreshnessUsesSourceTimeAndPreservesIt(t *testing.T) {
	for _, tc := range []struct {
		name string
		at   time.Time
		want Freshness
	}{{"old", sampleTime.Add(-3 * time.Minute), Old}, {"future", sampleTime.Add(time.Minute), Future}} {
		t.Run(tc.name, func(t *testing.T) {
			source := newTestSource(t, apiHandler(t, []string{"v1"}, func(w http.ResponseWriter, r *http.Request) {
				jsonResponse(t, w, listBody("v1", metricPod("app", tc.at, map[string]string{"cpu": "1n", "memory": "1"})))
			}), Options{})
			report, err := source.Read(t.Context())
			if err != nil || report.Availability != Stale || report.Observations[0].Freshness != tc.want || report.Observations[0].Timestamp != tc.at {
				t.Fatalf("%+v %v", report, err)
			}
		})
	}
}

func TestNumericJSONQuantitiesAndMissingUIDRemainCompatible(t *testing.T) {
	source := newTestSource(t, apiHandler(t, []string{"v1beta1"}, func(w http.ResponseWriter, r *http.Request) {
		pod := metricPod("app", sampleTime, nil).(map[string]any)
		delete(pod["metadata"].(map[string]string), "uid")
		pod["containers"] = []any{map[string]any{"name": "worker", "usage": map[string]any{"cpu": 0.125, "memory": 33554432}}}
		jsonResponse(t, w, listBody("v1beta1", pod))
	}), Options{})
	report, err := source.Read(t.Context())
	if err != nil || report.Availability != Available || len(report.Observations) != 1 {
		t.Fatalf("%+v %v", report, err)
	}
	value := report.Observations[0]
	if value.Identity.PodUID != "" || value.CPUUsageNanocores != 125000000 || value.MemoryWorkingSetBytes != 32<<20 {
		t.Fatal(value)
	}
}
