package promhistory

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

// The response body comes from the operator's Prometheus-compatible server.
// Whatever it holds, decoding must refuse or fill the report's existing grid:
// it may never add a series or a point the query did not ask for.
func FuzzDecodeResponse(f *testing.F) {
	selection := fixtureSelection()
	query := fixtureQuery(selection)
	valid, err := json.Marshal(fixtureResponse(selection, query))
	if err != nil {
		f.Fatal(err)
	}
	for _, seed := range [][]byte{valid, nil, []byte("{}"), []byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`),
		[]byte(`{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1,"1"]]}]}}`), valid[:len(valid)/2]} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64<<10 {
			return
		}
		report, err := memoryhistory.NewReport(selection, query, selection.ResolvedAt)
		if err != nil {
			t.Fatal(err)
		}
		series := len(report.Series)
		points := make([]int, series)
		for index := range report.Series {
			points[index] = len(report.Series[index].Points)
		}
		if decode(context.Background(), data, "cluster-a", &report) != nil {
			return
		}
		if len(report.Series) != series {
			t.Fatalf("decoding changed the series count from %d to %d", series, len(report.Series))
		}
		for index := range report.Series {
			if len(report.Series[index].Points) != points[index] {
				t.Fatalf("decoding changed series %d from %d points to %d", index, points[index], len(report.Series[index].Points))
			}
		}
	})
}
