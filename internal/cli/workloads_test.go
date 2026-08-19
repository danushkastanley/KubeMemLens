package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestPrintWorkloadsTableIncludesLargestReplica(t *testing.T) {
	output := &bytes.Buffer{}
	printWorkloadsTable(output, []api.WorkloadSnapshot{{
		Namespace: "default", Kind: "Deployment", Name: "api", PodCount: 2,
		LargestPodName: "api-b", LargestPodBytes: 300 << 20,
		Memory: model.MemoryBreakdown{TotalBytes: 400 << 20, AnonBytes: 350 << 20},
	}})
	for _, want := range []string{"NAMESPACE", "Deployment", "api-b", "300Mi", "rss-heavy"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("workload table missing %q:\n%s", want, output.String())
		}
	}
}

func TestPrintWorkloadExplanationKeepsReplicaRows(t *testing.T) {
	output := &bytes.Buffer{}
	workload := api.WorkloadSnapshot{
		Namespace: "default", Kind: "Deployment", Name: "api", PodCount: 2,
		LargestPodName: "api-b", LargestPodBytes: 300,
		Memory: model.MemoryBreakdown{TotalBytes: 400},
		Pods: []api.PodSnapshot{
			{PodName: "api-b", NodeName: "node-b", Memory: model.MemoryBreakdown{TotalBytes: 300}},
			{PodName: "api-a", NodeName: "node-a", Memory: model.MemoryBreakdown{TotalBytes: 100}},
		},
	}
	printWorkloadExplanation(output, workload)
	for _, want := range []string{"Workload: Deployment/default/api", "Confidence:", "Replicas: 2", "api-b", "api-a", "kubectl memlens history pod api-b"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("workload explanation missing %q:\n%s", want, output.String())
		}
	}
}
