package cli

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/collector"
)

// doctorAgainstNodes runs doctor against a collector that has fresh snapshots
// from the reporting nodes and expects the listed nodes to exist.
func doctorAgainstNodes(t *testing.T, reporting, expected []string, args ...string) (string, error) {
	t.Helper()
	now := time.Now().UTC()
	store := collector.NewStore()
	for _, node := range reporting {
		if _, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{
			NodeName: node, CapturedAt: now,
			Environment: api.NodeEnvironment{
				CgroupVersion: "v2", CgroupDriver: "systemd", ContainerRuntimes: []string{"containerd"},
				NodeContextAvailable: true, MemoryPressureStatus: "False", WorkloadContextAvailable: true,
			},
			Containers: []api.ContainerSnapshot{{Namespace: "default", PodName: "api-" + node, ContainerName: "app", ContainerID: "id-" + node}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if expected != nil {
		if err := store.ReconcileExpectedNodes(expected, now); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(collector.NewReadHandlerWithOptions(store, collector.DefaultHandlerOptions(time.Minute)))
	t.Cleanup(server.Close)
	output := &bytes.Buffer{}
	command := newDoctorCommand(func() client.Options {
		return client.Options{Mode: client.ConnectionModeHTTP, CollectorURL: server.URL}
	})
	command.SetOut(output)
	command.SetArgs(args)
	err := command.Execute()
	return output.String(), err
}

// A node whose agent never started sends nothing. The collector still lists it,
// marked missing, because the agent is expected there. Doctor must not count it
// as a fresh snapshot, and must not blame it for unreported environment detail.
func TestDoctorFailsWhenAnExpectedNodeHasNeverReported(t *testing.T) {
	output, err := doctorAgainstNodes(t, []string{"node-a", "node-b", "node-c"}, []string{"node-a", "node-b", "node-c", "node-d"})
	if err == nil {
		t.Fatalf("doctor passed with a node that has never reported:\n%s", output)
	}
	for _, want := range []string{
		"FAIL  agent coverage", "1 of 4 expected nodes have not reported a snapshot",
		"PASS  cgroup mode", "PASS  cgroup access", "PASS  runtime layout", "PASS  node pressure", "PASS  workload context",
		"node-d                   missing: no snapshot has been received from this node",
		"node-a                   fresh",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, output)
		}
	}
}

func TestDoctorPassesWhenEveryExpectedNodeReports(t *testing.T) {
	output, err := doctorAgainstNodes(t, []string{"node-a", "node-b"}, []string{"node-a", "node-b"})
	if err != nil || !strings.Contains(output, "PASS  agent coverage     2 node snapshots are fresh") || strings.Contains(output, "missing") {
		t.Fatalf("doctor error=%v output:\n%s", err, output)
	}
}

// With every expected node missing there is nothing fresh to count, and the
// message must still say how many nodes were expected.
func TestDoctorFailsWhenNoExpectedNodeHasReported(t *testing.T) {
	output, err := doctorAgainstNodes(t, nil, []string{"node-a", "node-b"})
	if err == nil || !strings.Contains(output, "FAIL  agent coverage") || !strings.Contains(output, "2 of 2 expected nodes have not reported a snapshot") {
		t.Fatalf("doctor error=%v output:\n%s", err, output)
	}
}

func TestDoctorJSONMarksTheMissingNode(t *testing.T) {
	output, err := doctorAgainstNodes(t, []string{"node-a"}, []string{"node-a", "node-b"}, "--output", "json")
	if err == nil || !strings.Contains(output, `"freshness": "missing"`) || !strings.Contains(output, `"name": "agent coverage"`) {
		t.Fatalf("doctor error=%v output:\n%s", err, output)
	}
}
