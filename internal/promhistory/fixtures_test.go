package promhistory

import (
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

func fixtureSelection() memoryhistory.Selection {
	at := time.Now().UTC().Truncate(time.Second)
	return memoryhistory.Selection{Request: memoryhistory.Request{Scope: memoryhistory.Pod, Namespace: "tenant-a", Name: "app"}, UID: "pod-uid", ResolvedAt: at,
		Targets: []memoryhistory.Target{{Namespace: "tenant-a", Pod: "app", PodUID: "pod-uid", Container: "app", ContainerID: "containerd://current", Node: "node-a", NodeUID: "node-uid", StartedAt: at.Add(-time.Hour)}}}
}
func fixtureQuery(s memoryhistory.Selection) memoryhistory.Query {
	return memoryhistory.Query{Source: memoryhistory.Prometheus, Metric: memoryhistory.WorkingSet, Start: s.ResolvedAt.Add(-2 * time.Minute), End: s.ResolvedAt, Step: time.Minute}
}
func fixtureClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewTLSServer(h)
	t.Cleanup(server.Close)
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	c, err := New(Options{URL: server.URL, Cluster: "cluster-a", CAData: ca, BearerToken: "test-token"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}
func fixtureResponse(s memoryhistory.Selection, q memoryhistory.Query) envelope {
	var out envelope
	out.Status = "success"
	out.Data.ResultType = "matrix"
	for _, target := range s.Targets {
		for _, field := range []string{"value", "sampled"} {
			labels := targetLabels("cluster-a", target)
			labels[fieldLabel] = field
			series := matrixSeries{Metric: labels}
			for at := q.Start; !at.After(q.End); at = at.Add(q.Step) {
				value := "0"
				if field == "sampled" {
					value = strconv.FormatInt(at.Unix(), 10)
				}
				stamp, _ := json.Marshal(at.Unix())
				text, _ := json.Marshal(value)
				series.Values = append(series.Values, []json.RawMessage{stamp, text})
			}
			out.Data.Result = append(out.Data.Result, series)
		}
	}
	return out
}
func respond(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
