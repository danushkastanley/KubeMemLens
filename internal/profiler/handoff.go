// Package profiler maps explicit workload declarations to reviewed static guidance.
// It never collects profiles, discovers endpoints or executes commands.
package profiler

import (
	"sort"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"k8s.io/apimachinery/pkg/util/validation"
)

const LabelPrefix = "profiling.kubememlens.io/"
const GoHeapProfile = "go-heap-pprof-v1"
const MaxContainers = 64

type Handoff struct {
	Profile       string   `json:"profile"`
	Runtime       string   `json:"runtime"`
	Basis         string   `json:"basis"`
	Prerequisites []string `json:"prerequisites"`
	Risks         []string `json:"risks"`
	Commands      []string `json:"commands"`
	Verification  []string `json:"verification"`
	References    []string `json:"references"`
}

type Result struct {
	Namespace string   `json:"namespace,omitempty"`
	Pod       string   `json:"pod,omitempty"`
	Container string   `json:"container,omitempty"`
	State     string   `json:"state"`
	Reason    string   `json:"reason"`
	Handoff   *Handoff `json:"handoff,omitempty"`
}

// ForPods consumes only snapshots already authorised for the caller. Labels are
// declarations by workload owners, never evidence of a running language process.
func ForPods(pods []api.PodSnapshot, now time.Time) []Result {
	if len(pods) > MaxContainers {
		return []Result{{State: "unavailable", Reason: "Profiler guidance exceeds the 64-Pod bound; select one Pod."}}
	}
	count := 0
	for _, pod := range pods {
		count += len(pod.Containers)
	}
	if count > MaxContainers {
		return []Result{{State: "unavailable", Reason: "Profiler guidance exceeds the 64-container bound; select one Pod."}}
	}
	results := make([]Result, 0, count)
	seen := make(map[[3]string]bool, count)
	for _, pod := range pods {
		for _, container := range pod.Containers {
			key := [3]string{pod.Namespace, pod.PodName, container.ContainerName}
			if seen[key] || !validIdentity(pod, container) {
				return []Result{{State: "unavailable", Reason: "Container identity is ambiguous or invalid; refresh authorised evidence."}}
			}
			seen[key] = true
			results = append(results, forContainer(pod, container, now))
		}
	}
	if len(results) == 0 {
		return []Result{{State: "unavailable", Reason: "No current container evidence is available for a profiler handoff."}}
	}
	sort.Slice(results, func(i, j int) bool {
		a, b := results[i], results[j]
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		if a.Pod != b.Pod {
			return a.Pod < b.Pod
		}
		return a.Container < b.Container
	})
	return results
}

func validIdentity(p api.PodSnapshot, c api.ContainerSnapshot) bool {
	return len(validation.IsDNS1123Label(p.Namespace)) == 0 &&
		len(validation.IsDNS1123Subdomain(p.PodName)) == 0 &&
		len(validation.IsDNS1123Label(c.ContainerName)) == 0 &&
		p.Namespace == c.Namespace && p.PodName == c.PodName &&
		p.PodUID != "" && p.PodUID == c.PodUID && c.ContainerID != ""
}

func forContainer(p api.PodSnapshot, c api.ContainerSnapshot, now time.Time) Result {
	r := Result{Namespace: p.Namespace, Pod: p.PodName, Container: c.ContainerName, State: "unavailable"}
	if p.Freshness != api.EvidenceFreshnessFresh || c.Freshness != api.EvidenceFreshnessFresh ||
		p.Completeness != api.EvidenceComplete || c.Completeness != api.EvidenceComplete ||
		p.Context.Phase != "Running" || c.Context.PodPhase != "Running" ||
		!current(p.CapturedAt, now) || !current(c.CapturedAt, now) {
		r.Reason = "Fresh, complete evidence for a running Pod is required; refresh before selecting a profiler."
		return r
	}
	declaration := c.Context.Labels[LabelPrefix+c.ContainerName]
	if declaration == "" {
		r.Reason = "No explicit supported runtime workflow is declared for this container."
		return r
	}
	if declaration != GoHeapProfile {
		r.Reason = "The declared runtime workflow is unsupported; use the workload owner's profiling runbook."
		return r
	}
	if explain.AnalyzeContainer(c).Diagnosis != explain.DiagnosisRSSHeavy {
		r.Reason = "Current evidence does not select anonymous-memory profiling; follow the primary investigation steps."
		return r
	}
	r.State = "available"
	r.Reason = "Anonymous memory is dominant; the owner declares a Go heap-profile workflow. This does not establish heap growth or a leak."
	r.Handoff = goHeap()
	return r
}

func current(observed, now time.Time) bool {
	return !now.IsZero() && !observed.IsZero() && now.Sub(observed) <= 30*time.Second && observed.Sub(now) <= 5*time.Second
}
