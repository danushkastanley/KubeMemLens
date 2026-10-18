package promhistory

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

func TestRejectsAmbiguousOrUnboundProviderEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*envelope)
	}{
		{"cross namespace", func(r *envelope) { r.Data.Result[0].Metric["namespace"] = "tenant-b" }},
		{"reused Pod name", func(r *envelope) { r.Data.Result[0].Metric["pod_uid"] = "older-instance" }},
		{"container restart", func(r *envelope) { r.Data.Result[0].Metric["container_id"] = "containerd://previous" }},
		{"missing container identity", func(r *envelope) { delete(r.Data.Result[0].Metric, "container_id") }},
		{"wrong cluster", func(r *envelope) { r.Data.Result[0].Metric["cluster"] = "cluster-b" }},
		{"duplicate series", func(r *envelope) { r.Data.Result = append(r.Data.Result, r.Data.Result[0]) }},
		{"timestamp collision", func(r *envelope) { r.Data.Result[1].Metric["job"] = "other-job" }},
		{"unpaired series", func(r *envelope) { r.Data.Result = r.Data.Result[:1] }},
		{"duplicate grid point", func(r *envelope) { r.Data.Result[0].Values[1] = r.Data.Result[0].Values[0] }},
		{"misaligned grid", func(r *envelope) { r.Data.Result[1].Values[1][0] = r.Data.Result[1].Values[0][0] }},
		{"negative bytes", func(r *envelope) { r.Data.Result[0].Values[0][1] = json.RawMessage(`"-1"`) }},
		{"infinite bytes", func(r *envelope) { r.Data.Result[0].Values[0][1] = json.RawMessage(`"+Inf"`) }},
		{"inexact integer bytes", func(r *envelope) { r.Data.Result[0].Values[0][1] = json.RawMessage(`"9007199254740992"`) }},
		{"invalid sample time", func(r *envelope) { r.Data.Result[1].Values[0][1] = json.RawMessage(`"1"`) }},
		{"not a matrix", func(r *envelope) { r.Data.ResultType = "vector" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixtureSelection()
			q := fixtureQuery(s)
			response := fixtureResponse(s, q)
			tc.change(&response)
			c := fixtureClient(t, func(w http.ResponseWriter, _ *http.Request) { respond(w, response) })
			r, err := c.Query(t.Context(), s, q)
			if err == nil || len(r.Series) != 0 {
				t.Fatal("untrusted evidence returned")
			}
		})
	}
}

func TestEmptyAndNaNRemainMissing(t *testing.T) {
	for _, empty := range []bool{true, false} {
		s := fixtureSelection()
		q := fixtureQuery(s)
		response := fixtureResponse(s, q)
		if empty {
			response.Data.Result = []matrixSeries{}
		} else {
			response.Data.Result[0].Values[1][1] = json.RawMessage(`"NaN"`)
		}
		c := fixtureClient(t, func(w http.ResponseWriter, _ *http.Request) { respond(w, response) })
		r, err := c.Query(t.Context(), s, q)
		if err != nil {
			t.Fatal(err)
		}
		if r.Completeness != memoryhistory.Partial || r.Series[0].Points[1].Bytes != nil {
			t.Fatal("missing value became zero")
		}
	}
}

func TestRejectsOversizeAndDuplicateJSON(t *testing.T) {
	for _, body := range []string{
		strings.Repeat(" ", memoryhistory.MaxResponseBytes+1),
		`{"status":"error","status":"success","data":{"resultType":"matrix","result":[]}}`,
		`{"status":"success","data":{"resultType":"matrix","result":[]},"DATA":{}}`,
		`{"status":"success","data":{"resultType":"matrix","result":[]},"extra":` + strings.Repeat("[", 12) + `0` + strings.Repeat("]", 12) + `}`,
	} {
		c := fixtureClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		})
		s := fixtureSelection()
		if _, err := c.Query(t.Context(), s, fixtureQuery(s)); err == nil {
			t.Fatal("invalid JSON accepted")
		}
	}
}

func TestInvalidQueryDoesNotReachProvider(t *testing.T) {
	calls := 0
	c := fixtureClient(t, func(w http.ResponseWriter, _ *http.Request) { calls++; w.WriteHeader(500) })
	s := fixtureSelection()
	q := fixtureQuery(s)
	q.Step = 0
	if _, err := c.Query(t.Context(), s, q); err == nil || calls != 0 {
		t.Fatal("invalid query reached provider")
	}
}

func TestConstructorRequiresVerifiedFixedEndpoint(t *testing.T) {
	for _, url := range []string{"http://localhost:9090", "https://user:pass@example.com", "https://example.com?query=up", "https://example.com/#fragment", "https://example.com/../other"} {
		if c, err := New(Options{URL: url, Cluster: "cluster-a"}); err == nil {
			c.Close()
			t.Fatal("unsafe endpoint accepted")
		}
	}
}

func TestEmptyPartialBackendRetainsItsWarningState(t *testing.T) {
	s := fixtureSelection()
	q := fixtureQuery(s)
	response := fixtureResponse(s, q)
	response.Data.Result = []matrixSeries{}
	response.Warnings = []string{"private backend failure"}
	c := fixtureClient(t, func(w http.ResponseWriter, _ *http.Request) { respond(w, response) })
	r, err := c.Query(t.Context(), s, q)
	if err != nil || r.Reason != "provider-partial" || r.State != memoryhistory.Missing || r.Completeness != memoryhistory.Partial {
		t.Fatalf("empty partial source: %+v %v", r, err)
	}
}

func TestProviderLabelMapsKeepCaseSensitiveNames(t *testing.T) {
	s := fixtureSelection()
	q := fixtureQuery(s)
	response := fixtureResponse(s, q)
	for i := range response.Data.Result {
		response.Data.Result[i].Metric["team"] = "a"
		response.Data.Result[i].Metric["Team"] = "b"
	}
	c := fixtureClient(t, func(w http.ResponseWriter, _ *http.Request) { respond(w, response) })
	if _, err := c.Query(t.Context(), s, q); err != nil {
		t.Fatal("distinct Prometheus labels were treated as aliases", err)
	}
}
