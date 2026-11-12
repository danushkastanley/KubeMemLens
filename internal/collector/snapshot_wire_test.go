package collector

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

func TestBoundSnapshotWire(t *testing.T) {
	deep := strings.Repeat(`{"a":`, maxSnapshotWireDepth+1) + `1` + strings.Repeat(`}`, maxSnapshotWireDepth+1)
	tests := map[string]struct {
		body string
		want error
	}{
		"no containers":           {body: `{"schemaVersion":1,"nodeName":"node-a"}`},
		"null containers":         {body: `{"containers":null}`},
		"at the limit":            {body: `{"containers":[{},{}]}`},
		"nested at the limit":     {body: `{"snapshot":{"containers":[{},{}]}}`},
		"other arrays unbounded":  {body: `{"environment":{"containerRuntimes":["a","b","c"]}}`},
		"scalar document":         {body: `null`},
		"over the limit":          {body: `{"containers":[{},{},{}]}`, want: ErrSnapshotContainerLimit},
		"nested over the limit":   {body: `{"snapshot":{"containers":[{},{},{}]}}`, want: ErrSnapshotContainerLimit},
		"case-insensitive alias":  {body: `{"CONTAINERS":[{},{},{}]}`, want: ErrSnapshotContainerLimit},
		"later duplicate bounded": {body: `{"containers":[],"Containers":[{},{},{}]}`, want: ErrSnapshotContainerLimit},
		"escaped key":             {body: `{"cont\u0061iners":[{},{},{}]}`, want: ErrSnapshotContainerLimit},
		"folded key":              {body: `{"containerſ":[{},{},{}]}`, want: ErrSnapshotContainerLimit},
		"null elements":           {body: `{"containers":[null,null,null]}`, want: ErrSnapshotContainerLimit},
		"scalar elements":         {body: `{"containers":[0, 0 ,0]}`, want: ErrSnapshotContainerLimit},
		"spaced over the limit":   {body: "{ \"containers\" :\n[ {} ,\t{} , {} ] }", want: ErrSnapshotContainerLimit},
		"brackets inside strings": {body: `{"nodeName":"a\"],[{","containers":[{"podName":"a,b,c"},{}]}`},
		"nested arrays uncounted": {body: `{"containers":[{"labels":[1,2,3,4]},{}]}`},
		"containers as a value":   {body: `{"nodeName":"containers","environment":{"containerRuntimes":["a","b","c"]}}`},
		"truncated":               {body: `{"containers":[{}`, want: ErrSnapshotWire},
		"unterminated string":     {body: `{"containers`, want: ErrSnapshotWire},
		"unbalanced close":        {body: `]`, want: ErrSnapshotWire},
		"nested too deeply":       {body: deep, want: ErrSnapshotWire},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if err := BoundSnapshotWire([]byte(test.body), 2); !errors.Is(err, test.want) {
				t.Fatalf("BoundSnapshotWire = %v, want %v", err, test.want)
			}
		})
	}
}

// Any valid document the bound accepts must not decode to more containers than
// the limit, in either the legacy or the authenticated envelope.
func FuzzBoundSnapshotWire(f *testing.F) {
	f.Add([]byte(`{"containers":[{},{}]}`), uint8(2))
	f.Add([]byte(`{"snapshot":{"containers":[{},{},{}]}}`), uint8(2))
	f.Add([]byte(`{"containers":[],"Containers":[{},{},{}]}`), uint8(1))
	f.Add([]byte(`{"containers":[{}`), uint8(4))
	f.Add([]byte(`{"cont\u0061iners":[null,0,{}],"nodeName":"a\"],["}`), uint8(2))
	f.Fuzz(func(t *testing.T, data []byte, rawLimit uint8) {
		limit := int(rawLimit % 8)
		if len(data) > maxFuzzSnapshotBytes || BoundSnapshotWire(data, limit) != nil {
			return
		}
		if !json.Valid(data) {
			return
		}
		// Type errors do not stop encoding/json from allocating elements, so
		// the count is checked whether or not the typed decode succeeds.
		var legacy api.AgentSnapshot
		_ = json.Unmarshal(data, &legacy)
		if len(legacy.Containers) > limit {
			t.Fatalf("accepted %d legacy containers with a limit of %d", len(legacy.Containers), limit)
		}
		var authenticated api.NodeSnapshotRequest
		_ = json.Unmarshal(data, &authenticated)
		if len(authenticated.Snapshot.Containers) > limit {
			t.Fatalf("accepted %d authenticated containers with a limit of %d", len(authenticated.Snapshot.Containers), limit)
		}
	})
}
