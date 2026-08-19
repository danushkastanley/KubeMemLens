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

func TestDoctorReportsFreshNodesAndMappingWarning(t *testing.T) {
	now := time.Now().UTC()
	store := collector.NewStore()
	store.ReplaceNodeSnapshot(api.AgentSnapshot{
		NodeName:   "node-a",
		CapturedAt: now,
		Environment: api.NodeEnvironment{
			CgroupVersion:            "v2",
			CgroupDriver:             "systemd",
			ContainerRuntimes:        []string{"containerd"},
			NodeContextAvailable:     true,
			MemoryPressureStatus:     "False",
			WorkloadContextAvailable: true,
		},
		Containers: []api.ContainerSnapshot{
			{
				Namespace:     "default",
				PodName:       "api",
				ContainerName: "app",
				ContainerID:   "id-a",
			},
			{ContainerID: "unmapped-id"},
		},
	})
	server := httptest.NewServer(collector.NewReadHandlerWithOptions(store, collector.DefaultHandlerOptions(time.Minute)))
	defer server.Close()
	opts := func() client.Options {
		return client.Options{Mode: client.ConnectionModeHTTP, CollectorURL: server.URL}
	}

	output := &bytes.Buffer{}
	cmd := newDoctorCommand(opts)
	cmd.SetOut(output)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("doctor returned error for non-strict warning: %v", err)
	}
	for _, want := range []string{"PASS  agent coverage", "PASS  cgroup mode", "PASS  runtime layout", "PASS  node pressure", "PASS  collector bounds", "WARN  mapping coverage", "1/2 (50.0%)"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("doctor output missing %q:\n%s", want, output.String())
		}
	}

	strict := newDoctorCommand(opts)
	strict.SetOut(&bytes.Buffer{})
	strict.SetArgs([]string{"--strict"})
	if err := strict.Execute(); err == nil {
		t.Fatal("strict doctor returned nil error for mapping warning")
	}
}

func TestDoctorFailsForCgroupReadErrors(t *testing.T) {
	now := time.Now().UTC()
	store := collector.NewStore()
	_, err := store.ReplaceNodeSnapshot(api.AgentSnapshot{
		NodeName:   "node-a",
		CapturedAt: now,
		Environment: api.NodeEnvironment{
			CgroupVersion:            "v2",
			CgroupDriver:             "systemd",
			ContainerRuntimes:        []string{"containerd"},
			CgroupReadErrors:         1,
			NodeContextAvailable:     true,
			MemoryPressureStatus:     "False",
			WorkloadContextAvailable: true,
		},
		Containers: []api.ContainerSnapshot{{
			Namespace:     "default",
			PodName:       "api",
			ContainerName: "app",
			ContainerID:   "id-a",
		}},
	})
	if err != nil {
		t.Fatalf("ReplaceNodeSnapshot returned error: %v", err)
	}
	server := httptest.NewServer(collector.NewReadHandlerWithOptions(store, collector.DefaultHandlerOptions(time.Minute)))
	defer server.Close()
	cmd := newDoctorCommand(func() client.Options {
		return client.Options{Mode: client.ConnectionModeHTTP, CollectorURL: server.URL}
	})
	output := &bytes.Buffer{}
	cmd.SetOut(output)
	if err := cmd.Execute(); err == nil {
		t.Fatal("doctor returned nil error for cgroup read errors")
	}
	if !strings.Contains(output.String(), "FAIL  cgroup access") {
		t.Fatalf("unexpected doctor output:\n%s", output.String())
	}
}

func TestDoctorFailsWithoutAgentSnapshots(t *testing.T) {
	server := httptest.NewServer(collector.NewReadHandlerWithOptions(collector.NewStore(), collector.DefaultHandlerOptions(time.Minute)))
	defer server.Close()
	cmd := newDoctorCommand(func() client.Options {
		return client.Options{Mode: client.ConnectionModeHTTP, CollectorURL: server.URL}
	})
	output := &bytes.Buffer{}
	cmd.SetOut(output)

	if err := cmd.Execute(); err == nil {
		t.Fatal("doctor returned nil error without agent snapshots")
	}
	if !strings.Contains(output.String(), "FAIL  agent coverage") {
		t.Fatalf("unexpected doctor output:\n%s", output.String())
	}
}
