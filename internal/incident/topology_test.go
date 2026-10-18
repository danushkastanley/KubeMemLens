package incident

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/memorytopology"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func topologyFixture(t *testing.T) (NodeBundle, api.NodeMemoryTopology) {
	t.Helper()
	node := nodeFixture(t)
	o := memorytopology.Failure(node.Evidence.Record.NodeName, node.Evidence.Record.NodeUID, node.CapturedAt)
	total, free := uint64(1<<30), uint64(512<<20)
	o.NUMA = memorytopology.Section[memorytopology.NUMANode]{Source: memorytopology.NUMASysfs, Availability: capability.Available, Completeness: capability.Partial, Reason: memorytopology.PartialFields, CapturedAt: node.CapturedAt, Items: []memorytopology.NUMANode{{ID: 4, TotalBytes: &total, FreeBytes: &free, Pools: []memorytopology.HugePool{}}, {ID: 8, TotalBytes: &total, FreeBytes: &free, Pools: []memorytopology.HugePool{}}}}
	report, err := memorytopology.Analyse(o, node.CapturedAt)
	if err != nil {
		t.Fatal(err)
	}
	return node, api.NodeMemoryTopology{TypeMeta: metav1.TypeMeta{APIVersion: api.MemoryAPIGroup + "/" + api.MemoryAPIVersion, Kind: "NodeMemoryTopology"}, ObjectMeta: metav1.ObjectMeta{Name: node.Evidence.Record.NodeName}, Current: &report, LastGood: &report}
}
func TestTopologyCaptureRedactsAndPreservesSource(t *testing.T) {
	node, value := topologyFixture(t)
	before, _ := json.Marshal(value)
	beforeNode, _ := json.Marshal(node)
	b, err := NewTopology(node, value, false)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"worker-a", "private-node-uid", "private-namespace", "private-pod", "private-workload", `"id":4`, `"id":8`} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("capture leaked %s", secret)
		}
	}
	if b.Node.SchemaVersion != 4 || !b.Redacted || b.Current.Observation.NUMA.Items[0].ID != 0 || b.Current.Observation.NUMA.Items[1].ID != 1 || b.Current.NUMA.State != value.Current.NUMA.State {
		t.Fatal("redaction changed interpretation")
	}
	after, _ := json.Marshal(value)
	afterNode, _ := json.Marshal(node)
	if !bytes.Equal(before, after) || !bytes.Equal(beforeNode, afterNode) {
		t.Fatal("export mutated live data")
	}
	path := filepath.Join(t.TempDir(), "topology.json")
	if err := WriteTopology(io.Discard, path, false, b); err != nil {
		t.Fatal(err)
	}
	doc, err := Read(path)
	if err != nil || doc.Topology == nil || doc.Node != nil || doc.Deep != nil {
		t.Fatalf("schema dispatch: %+v %v", doc, err)
	}
	if err := WriteTopology(io.Discard, path, false, b); err == nil {
		t.Fatal("existing file overwritten without opt-in")
	}
	sensitive, err := NewTopology(node, value, true)
	if err != nil || sensitive.Current.Observation.NUMA.Items[0].ID != 4 || sensitive.Redacted {
		t.Fatal("sensitive capture altered source identity")
	}
}
func TestTopologyCaptureRejectsScopeAndTampering(t *testing.T) {
	node, value := topologyFixture(t)
	value.Current.Observation.NodeUID = "wrong-node"
	if _, err := NewTopology(node, value, false); err == nil {
		t.Fatal("mixed Node UIDs accepted")
	}
	node, value = topologyFixture(t)
	b, err := NewTopology(node, value, false)
	if err != nil {
		t.Fatal(err)
	}
	b.Current.NUMA.State = "balanced"
	if ValidateTopology(b) == nil {
		t.Fatal("invented analysis accepted")
	}
	node, value = topologyFixture(t)
	b, err = NewTopology(node, value, false)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(b)
	for _, invalid := range [][]byte{
		bytes.Replace(raw, []byte(`"schemaVersion":7`), []byte(`"schemaVersion":7,"schemaVersion":7`), 1),
		bytes.Replace(raw, []byte(`"redacted":true,`), nil, 1),
		append(append([]byte(nil), raw...), []byte(`{}`)...),
	} {
		if _, err := decodeTopology(invalid); err == nil {
			t.Fatal("invalid wire accepted")
		}
	}
}
