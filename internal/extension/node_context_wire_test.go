package extension

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

func TestNodeWireBoundsSharedEnvelopeBeforeTypedAllocation(t *testing.T) {
	data, err := json.Marshal(nodeRequest(time.Now().UTC(), 1))
	if err != nil {
		t.Fatal(err)
	}
	if err := boundedNodeWire(data); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"snapshot":{"containers":[{}]}}`,
		`{"snapshot":{"containers":[{}],"Containers":[]}}`,
		`{"metadata":{"ownerReferences":[{}]}}`,
		`{"metadata":{"managedFields":[{}]}}`,
		`{"snapshot":{"environment":{"containerRuntimes":["runtime"]}}}`,
		`{"snapshot":{"nodeContext":{"stats":{"systemContainers":[{},{},{},{},{}]}}}}`,
		`{"snapshot":{"containers":[` + strings.Repeat(`{},`, 4000) + `{}]}}`,
	} {
		if err := boundedNodeWire([]byte(body)); err == nil {
			t.Fatal("allocating non-Node envelope accepted")
		}
	}
}

func FuzzNodeWire(f *testing.F) {
	data, _ := json.Marshal(nodeRequest(time.Unix(1800000000, 0).UTC(), 1))
	f.Add(data)
	f.Add([]byte(`{"snapshot":{"containers":[{}]}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 24<<10 || boundedNodeWire(data) != nil {
			return
		}
		var request api.NodeSnapshotRequest
		if json.Unmarshal(data, &request) != nil {
			return
		}
		if len(request.Snapshot.Containers) != 0 || len(request.Snapshot.Environment.ContainerRuntimes) != 0 {
			t.Fatal("Node wire admitted cgroup collections")
		}
	})
}
