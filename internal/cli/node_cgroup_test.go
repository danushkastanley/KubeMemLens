package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/collector"
)

const cgroupV1Summary = "1 of 2 reporting nodes use cgroup v1; KubeMemLens reads cgroup v2 only, so those nodes have no container memory evidence (see docs/compatibility.md)"

// cgroupVersionHandler serves fresh snapshots from node-a on cgroup v2 with
// one container and from node-b with the given version and no containers.
func cgroupVersionHandler(t *testing.T, nodeBVersion string) http.Handler {
	t.Helper()
	now := time.Now().UTC()
	store := collector.NewStore()
	environment := api.NodeEnvironment{
		CgroupVersion: "v2", CgroupDriver: "systemd", ContainerRuntimes: []string{"containerd"},
		NodeContextAvailable: true, MemoryPressureStatus: "False", WorkloadContextAvailable: true,
	}
	snapshots := []api.AgentSnapshot{{
		NodeName: "node-a", CapturedAt: now, Environment: environment,
		Containers: []api.ContainerSnapshot{{Namespace: "default", PodName: "api", ContainerName: "app", ContainerID: "id-a"}},
	}}
	environment.CgroupVersion, environment.CgroupDriver, environment.ContainerRuntimes = nodeBVersion, "", []string{"unknown"}
	snapshots = append(snapshots, api.AgentSnapshot{NodeName: "node-b", CapturedAt: now, Environment: environment})
	for _, snapshot := range snapshots {
		if _, err := store.ReplaceNodeSnapshot(snapshot); err != nil {
			t.Fatal(err)
		}
	}
	return collector.NewReadHandlerWithOptions(store, collector.DefaultHandlerOptions(time.Minute))
}

func serveCollector(t *testing.T, handler http.Handler) func() client.Options {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return func() client.Options {
		return client.Options{Mode: client.ConnectionModeHTTP, CollectorURL: server.URL}
	}
}

func runDoctor(t *testing.T, opts func() client.Options) (string, error) {
	t.Helper()
	output := &bytes.Buffer{}
	command := newDoctorCommand(opts)
	command.SetOut(output)
	err := command.Execute()
	return output.String(), err
}

func runStatus(t *testing.T, opts func() client.Options, args ...string) string {
	t.Helper()
	output := &bytes.Buffer{}
	command := newStatusCommand(opts)
	command.SetOut(output)
	command.SetArgs(args)
	if err := command.Execute(); err != nil {
		t.Fatalf("status %v returned error: %v", args, err)
	}
	return output.String()
}

func TestDoctorAndStatusNameCgroupV1Nodes(t *testing.T) {
	opts := serveCollector(t, cgroupVersionHandler(t, "v1"))

	doctor, err := runDoctor(t, opts)
	if err == nil {
		t.Fatalf("doctor passed with a cgroup v1 node:\n%s", doctor)
	}
	for _, want := range []string{
		"FAIL  cgroup mode        " + cgroupV1Summary,
		"PASS  cgroup access",
		"node-b                   fresh age=",
		"containers=0 cgroup=v1/unknown",
	} {
		if !strings.Contains(doctor, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, doctor)
		}
	}
	if strings.Contains(doctor, "did not report") {
		t.Fatalf("doctor described a cgroup v1 node as unreported:\n%s", doctor)
	}

	status := runStatus(t, opts)
	if !strings.Contains(status, "  history completeness: partial\n  cgroup version: "+cgroupV1Summary+"\n") {
		t.Fatalf("status output does not name the cgroup v1 node in its Reliability block:\n%s", status)
	}
}

// An empty version comes from an older agent or another producer. Doctor keeps
// its earlier warning for it, and neither command calls it cgroup v1.
func TestDoctorAndStatusKeepAnUnreportedCgroupVersionApartFromV1(t *testing.T) {
	opts := serveCollector(t, cgroupVersionHandler(t, ""))
	const unreported = "1 reporting nodes did not report their cgroup version"

	doctor, err := runDoctor(t, opts)
	if err != nil {
		t.Fatalf("non-strict doctor failed for an unreported cgroup version: %v\n%s", err, doctor)
	}
	for _, want := range []string{
		"WARN  cgroup mode        " + unreported + "\n",
		"WARN  cgroup access      cgroup read access was not reported by every node",
	} {
		if !strings.Contains(doctor, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, doctor)
		}
	}

	status := runStatus(t, opts)
	if !strings.Contains(status, "  cgroup version: "+unreported+"\n") || strings.Contains(doctor+status, "cgroup v1") {
		t.Fatalf("doctor or status described an unreported cgroup version as v1:\n%s\n%s", doctor, status)
	}
}

// With every node on cgroup v2, status renders exactly what it rendered before
// the node read was added.
func TestStatusAddsNothingWhenEveryNodeUsesCgroupV2(t *testing.T) {
	opts := serveCollector(t, cgroupVersionHandler(t, "v2"))
	scoped, err := withReadScope(opts(), "", true)
	if err != nil {
		t.Fatal(err)
	}
	report, err := buildStatusReport(context.Background(), scoped)
	if err != nil {
		t.Fatal(err)
	}
	withoutNodeRead := report
	withoutNodeRead.cgroup = statusCgroup{}
	text := renderStatusReport(report)
	if text != renderStatusReport(withoutNodeRead) || !strings.Contains(text, "  fresh nodes: 2\n") || strings.Contains(text, "cgroup version") {
		t.Fatalf("status output for cgroup v2 nodes changed:\n%s", text)
	}
}

// The node read is supplementary. A caller who can read the cluster status but
// not the node list keeps the whole report, its exit status and its JSON.
func TestStatusStillReportsWhenNodesCannotBeRead(t *testing.T) {
	collectorHandler := cgroupVersionHandler(t, "v1")
	opts := serveCollector(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/nodes" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		collectorHandler.ServeHTTP(w, r)
	}))

	text := runStatus(t, opts)
	for _, want := range []string{
		"  missing nodes: 0\n",
		"  history completeness: partial\n  cgroup version: not checked; node status could not be read\n\nMetrics:",
		"Data:\n  status: ready\n",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("status output missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Error:") || strings.Contains(text, "forbidden") || strings.Contains(text, opts().CollectorURL+"/api") {
		t.Fatalf("status output carries the node read error:\n%s", text)
	}

	json := runStatus(t, opts, "--output=json")
	if strings.Contains(json, "cgroup version") || strings.Contains(json, "not checked") || strings.Contains(json, `"error"`) || !strings.Contains(json, `"status": "ready"`) {
		t.Fatalf("status JSON changed when nodes could not be read:\n%s", json)
	}
}
