package resourcemetrics

import (
	"net/http"
	"strings"
	"testing"

	"k8s.io/client-go/rest"
)

func TestPaginationAndCaps(t *testing.T) {
	reads := 0
	makeSource := func(options Options) Source {
		return newTestSource(t, apiHandler(t, []string{"v1"}, func(w http.ResponseWriter, r *http.Request) {
			reads++
			if reads == 1 {
				body := listBody("v1", metricPod("a", sampleTime, map[string]string{"cpu": "1m", "memory": "1Ki"})).(map[string]any)
				body["metadata"] = map[string]string{"continue": "next/token"}
				jsonResponse(t, w, body)
				return
			}
			if r.URL.Query().Get("continue") != "next/token" {
				t.Error("cursor changed")
			}
			jsonResponse(t, w, listBody("v1", metricPod("b", sampleTime, map[string]string{"cpu": "1m", "memory": "2Ki"})))
		}), options)
	}
	source := makeSource(Options{})
	report, err := source.Read(t.Context())
	if err != nil || reads != 2 || len(report.Observations) != 2 || report.Availability != Available {
		t.Fatalf("%+v %v", report, err)
	}
	for _, opts := range []Options{{MaxPages: 1}, {MaxPods: 1}, {MaxContainers: 1}} {
		reads = 0
		bounded := makeSource(opts)
		report, err = bounded.Read(t.Context())
		if err != nil || report.Availability != Partial || report.Reason != LimitReached || len(report.Observations) != 1 || reads > 2 {
			t.Fatalf("cap: %+v %v", report, err)
		}
	}
}

func TestNamespaceViolationAndDuplicateDataFailClosed(t *testing.T) {
	for _, mutate := range []func(map[string]any){
		func(p map[string]any) { p["metadata"].(map[string]string)["namespace"] = "other-tenant" },
		func(p map[string]any) { p["metadata"].(map[string]string)["name"] = "../other" },
		func(p map[string]any) {
			p["containers"] = []any{map[string]any{"name": "worker", "usage": map[string]string{"cpu": "1m", "memory": "1"}}, map[string]any{"name": "worker", "usage": map[string]string{"cpu": "1m", "memory": "1"}}}
		},
	} {
		source := newTestSource(t, apiHandler(t, []string{"v1"}, func(w http.ResponseWriter, r *http.Request) {
			bad := metricPod("bad", sampleTime, map[string]string{"cpu": "1m", "memory": "99Mi"}).(map[string]any)
			mutate(bad)
			jsonResponse(t, w, listBody("v1", metricPod("valid", sampleTime, map[string]string{"cpu": "1m", "memory": "1Mi"}), bad))
		}), Options{})
		report, err := source.Read(t.Context())
		if err != nil || report.Availability != Unavailable || len(report.Observations) != 0 {
			t.Fatalf("out-of-scope/invalid data escaped: %+v %v", report, err)
		}
	}
}

func TestRepeatedCursorStopsAndRevocationDropsEarlierPages(t *testing.T) {
	for _, denied := range []bool{false, true} {
		reads := 0
		source := newTestSource(t, apiHandler(t, []string{"v1"}, func(w http.ResponseWriter, r *http.Request) {
			reads++
			if denied && reads == 2 {
				w.WriteHeader(403)
				return
			}
			name := "a"
			if reads == 2 {
				name = "b"
			}
			body := listBody("v1", metricPod(name, sampleTime, map[string]string{"cpu": "1m", "memory": "1"})).(map[string]any)
			body["metadata"] = map[string]string{"continue": "repeat"}
			jsonResponse(t, w, body)
		}), Options{})
		report, err := source.Read(t.Context())
		if err != nil || len(report.Observations) != 0 || reads != 2 {
			t.Fatalf("%+v %v reads=%d", report, err, reads)
		}
		want := Unavailable
		if denied {
			want = Forbidden
		}
		if report.Availability != want {
			t.Fatal(report)
		}
	}
}

func TestResponseAndConfigurationBounds(t *testing.T) {
	source := newTestSource(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(strings.Repeat("x", 300))) }, Options{MaxResponseBytes: 128})
	report, err := source.Read(t.Context())
	if err == nil || report.Availability != Unavailable || report.Reason != InvalidResponse {
		t.Fatalf("%+v %v", report, err)
	}
	for _, opts := range []Options{{Namespace: ""}, {Namespace: "../other"}, {Namespace: "team-a", MaxPages: -1}, {Namespace: "team-a", MaxResponseBytes: 1 << 40}} {
		if _, err := New(&rest.Config{Host: "https://example.invalid"}, opts); err == nil {
			t.Fatal("invalid bound/scope accepted")
		}
	}
}

func TestQuantitiesRemainBoundedAndPrecise(t *testing.T) {
	for _, text := range []string{"", "-1", "not-a-quantity", "1e2147483647", "1e-2147483647", strings.Repeat("9", 129)} {
		if _, ok := usageValue(text, 9); ok {
			t.Fatalf("invalid quantity %q accepted", text)
		}
	}
	for text, want := range map[string]uint64{"1n": 1, "250m": 250000000, "1": 1000000000} {
		if got, ok := usageValue(text, 9); !ok || got != want {
			t.Fatalf("%s = %d, %v", text, got, ok)
		}
	}
}
