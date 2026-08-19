package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestPrintPodComparisonShowsCompositionAndRate(t *testing.T) {
	output := &bytes.Buffer{}
	before := api.PodSnapshot{Memory: model.MemoryBreakdown{TotalBytes: 100 << 20, AnonBytes: 80 << 20}}
	after := api.PodSnapshot{Memory: model.MemoryBreakdown{TotalBytes: 160 << 20, AnonBytes: 100 << 20, ShmemBytes: 20 << 20}}
	printPodComparison(output, "comparison", before, after, time.Minute)
	for _, want := range []string{"SIGNAL", "Total", "+60Mi", "Shmem / tmpfs", "Residual / other", "Slab reclaimable", "Socket memory", "Page tables", "Mapped file", "THP anon", "Diagnosis:", "Total rate: +1Mi/s"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("comparison missing %q:\n%s", want, output.String())
		}
	}
}

func TestLivePodNameAcceptsKubectlResourceSyntax(t *testing.T) {
	for input, want := range map[string]string{"api": "api", "pod/api": "api", "Pod/api": "api"} {
		got, err := livePodName(input)
		if err != nil || got != want {
			t.Fatalf("livePodName(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := livePodName("default/api"); err == nil {
		t.Fatal("livePodName accepted ambiguous namespace/name syntax")
	}
}

func TestSignedBytesHandlesGrowthAndReduction(t *testing.T) {
	if got := signedBytes(100, 150); got != "+50B" {
		t.Fatalf("growth = %q, want +50B", got)
	}
	if got := signedBytes(150, 100); got != "-50B" {
		t.Fatalf("reduction = %q, want -50B", got)
	}
}

func TestIncidentWorkloadAggregatesEveryReplica(t *testing.T) {
	bundle := api.IncidentBundle{Pods: []api.PodSnapshot{
		{Namespace: "default", PodName: "api-a", Context: api.PodContext{WorkloadKind: "Deployment", WorkloadName: "api"}, Memory: model.MemoryBreakdown{TotalBytes: 100}},
		{Namespace: "default", PodName: "api-b", Context: api.PodContext{WorkloadKind: "Deployment", WorkloadName: "api"}, Memory: model.MemoryBreakdown{TotalBytes: 200}},
		{Namespace: "other", PodName: "api-c", Context: api.PodContext{WorkloadKind: "Deployment", WorkloadName: "api"}, Memory: model.MemoryBreakdown{TotalBytes: 400}},
	}}
	workload, ok := incidentWorkload(bundle, "default/deployment/api")
	if !ok || workload.Memory.TotalBytes != 300 {
		t.Fatalf("unexpected incident workload: ok=%v workload=%#v", ok, workload)
	}
	if _, ok := incidentWorkload(bundle, "default/deployment/missing"); ok {
		t.Fatal("missing workload was found")
	}
}
