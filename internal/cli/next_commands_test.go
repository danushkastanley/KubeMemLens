package cli

import (
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

// A tenant controls the kind and name in a Pod's owner reference. Neither may
// reach a line the operator is invited to copy into a shell.
func TestNextCommandsLeaveOutNamesThatAreNotKubernetesNames(t *testing.T) {
	const hostile = "x; touch marker #"
	pod := api.PodSnapshot{Namespace: "team-a", PodName: "api",
		Context: api.PodContext{WorkloadKind: "Deployment", WorkloadName: hostile}}
	commands := podNextCommands(pod)
	if joined := strings.Join(commands, "\n"); strings.Contains(joined, "marker") {
		t.Fatalf("Pod next commands carry the owner name:\n%s", joined)
	}
	if len(commands) != 3 {
		t.Fatalf("the three Pod commands should remain, got %q", commands)
	}

	pod.Context.WorkloadName, pod.Context.WorkloadKind = "api", "Deploy ment;id"
	if joined := strings.Join(podNextCommands(pod), "\n"); strings.Contains(joined, ";") {
		t.Fatalf("Pod next commands carry the owner kind:\n%s", joined)
	}

	workload := api.WorkloadSnapshot{Namespace: "team-a", Kind: "Deployment", Name: hostile, LargestPodName: "api"}
	joined := strings.Join(workloadNextCommands(workload), "\n")
	if strings.Contains(joined, "marker") {
		t.Fatalf("workload next commands carry the workload name:\n%s", joined)
	}
	if !strings.Contains(joined, "kubectl memlens explain pod api -n team-a") {
		t.Fatalf("the Pod commands should remain:\n%s", joined)
	}

	pod.Context.WorkloadKind = "Deployment"
	if got := podNextCommands(pod); len(got) != 4 || got[3] != "kubectl memlens explain workload deployment/api -n team-a" {
		t.Fatalf("ordinary names changed: %q", got)
	}
}
