package incidentsessionapi

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

func TestMarkerQueryWireRoundTrip(t *testing.T) {
	input := struct {
		Pod   string              `json:"pod"`
		Query memoryhistory.Query `json:"query"`
	}{Pod: "api", Query: memoryhistory.Query{Source: memoryhistory.Prometheus, Metric: memoryhistory.WorkingSet, Start: time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 30, 10, 1, 0, 0, time.UTC), Step: time.Minute}}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Pod   string              `json:"pod"`
		Query memoryhistory.Query `json:"query"`
	}
	request := httptest.NewRequest("POST", "/", bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	if err := readBody(request, &got); err != nil {
		t.Fatal(err)
	}
	if got.Query != input.Query {
		t.Fatal("query changed")
	}
}
