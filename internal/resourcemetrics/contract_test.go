package resourcemetrics

import (
	"net/http"
	"testing"
	"time"
)

func TestEmptyProviderResponseDoesNotClaimNamespaceCoverage(t *testing.T) {
	source := newTestSource(t, apiHandler(t, []string{"v1"}, func(w http.ResponseWriter, r *http.Request) { jsonResponse(t, w, listBody("v1")) }), Options{})
	report, err := source.Read(t.Context())
	if err != nil || report.Availability != Available || len(report.Observations) != 0 || report.ProviderCoverageKnown {
		t.Fatalf("%+v %v", report, err)
	}
}

func TestMissingTimesAndMixedFreshnessArePartial(t *testing.T) {
	for _, invalidTime := range []bool{false, true} {
		source := newTestSource(t, apiHandler(t, []string{"v1"}, func(w http.ResponseWriter, r *http.Request) {
			other := metricPod("other", sampleTime.Add(-time.Hour), map[string]string{"cpu": "1m", "memory": "1Mi"}).(map[string]any)
			if invalidTime {
				delete(other, "timestamp")
			}
			jsonResponse(t, w, listBody("v1", metricPod("fresh", sampleTime, map[string]string{"cpu": "1m", "memory": "1Mi"}), other))
		}), Options{})
		report, err := source.Read(t.Context())
		if err != nil || report.Availability != Partial {
			t.Fatalf("%+v %v", report, err)
		}
		if invalidTime && (report.OmittedContainers != 1 || len(report.Observations) != 1) {
			t.Fatal(report)
		}
	}
}

func TestMalformedDiscoveryAndListVersionAreUnavailable(t *testing.T) {
	for _, mode := range []string{"unsupported-version", "wrong-group", "not-namespaced", "wrong-list-version"} {
		source := newTestSource(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/apis/"+group {
				switch mode {
				case "unsupported-version":
					jsonResponse(t, w, groupBody("v2"))
				case "wrong-group":
					jsonResponse(t, w, map[string]any{"name": "other", "versions": []any{}})
				default:
					jsonResponse(t, w, groupBody("v1"))
				}
				return
			}
			if r.URL.Path == "/apis/"+group+"/v1" {
				if mode == "not-namespaced" {
					jsonResponse(t, w, map[string]any{"groupVersion": group + "/v1", "resources": []any{map[string]any{"name": "pods", "namespaced": false, "verbs": []string{"list"}}}})
				} else {
					jsonResponse(t, w, resourceBody("v1"))
				}
				return
			}
			jsonResponse(t, w, listBody("v1beta1"))
		}, Options{})
		report, err := source.Read(t.Context())
		if err != nil || report.Availability != Unavailable || len(report.Observations) != 0 {
			t.Fatalf("%s: %+v %v", mode, report, err)
		}
	}
}
