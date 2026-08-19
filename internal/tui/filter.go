package tui

import (
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/explain"
)

func FilterNamespaces(items []api.NamespaceSnapshot, namespace string, allNamespaces bool, query string) []api.NamespaceSnapshot {
	query = strings.ToLower(strings.TrimSpace(query))
	filtered := make([]api.NamespaceSnapshot, 0, len(items))
	for _, item := range items {
		if !allNamespaces && namespace != "" && item.Namespace != namespace {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(item.Namespace), query) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func FilterPods(items []api.PodSnapshot, namespace string, allNamespaces bool, query string) []api.PodSnapshot {
	return FilterPodsAt(items, namespace, allNamespaces, query, time.Now(), 30*time.Second)
}

func FilterPodsAt(items []api.PodSnapshot, namespace string, allNamespaces bool, query string, now time.Time, staleAfter time.Duration) []api.PodSnapshot {
	spec := parseFilter(query)
	filtered := make([]api.PodSnapshot, 0, len(items))
	for _, item := range items {
		if !allNamespaces && namespace != "" && item.Namespace != namespace {
			continue
		}
		if !spec.matchesPod(item, now, staleAfter) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func FilterWorkloads(items []api.WorkloadSnapshot, namespace string, allNamespaces bool, query string) []api.WorkloadSnapshot {
	spec := parseFilter(query)
	filtered := make([]api.WorkloadSnapshot, 0, len(items))
	for _, item := range items {
		if !allNamespaces && namespace != "" && item.Namespace != namespace {
			continue
		}
		result := explain.AnalyzeWorkload(item)
		if spec.text != "" && !containsAny([]string{item.Namespace, item.Kind, item.Name, item.LargestPodName, string(result.Diagnosis)}, spec.text) {
			continue
		}
		if spec.owner != "" && !containsAny([]string{item.Kind, item.Name}, spec.owner) {
			continue
		}
		if !matchesMemoryFilters(spec, item.Memory, string(result.Severity), string(result.Diagnosis)) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func FilterContainers(items []api.ContainerSnapshot, namespace string, allNamespaces bool, query string) []api.ContainerSnapshot {
	spec := parseFilter(query)
	filtered := make([]api.ContainerSnapshot, 0, len(items))
	for _, item := range items {
		if item.Namespace == "" || item.PodName == "" || item.ContainerName == "" {
			continue
		}
		if !allNamespaces && namespace != "" && item.Namespace != namespace {
			continue
		}
		result := explain.AnalyzeContainer(item)
		if spec.text != "" && !containerMatches(item, spec.text) {
			continue
		}
		if spec.owner != "" && !containsAny([]string{item.Context.OwnerKind, item.Context.OwnerName, item.Context.WorkloadKind, item.Context.WorkloadName}, spec.owner) {
			continue
		}
		if !matchesMemoryFilters(spec, item.Memory, string(result.Severity), string(result.Diagnosis)) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func podMatches(item api.PodSnapshot, query string) bool {
	values := []string{
		item.Namespace,
		item.PodName,
		item.NodeName,
		string(explain.Analyze(item.Memory).Diagnosis),
	}
	return containsAny(values, query)
}

func containerMatches(item api.ContainerSnapshot, query string) bool {
	values := []string{
		item.Namespace,
		item.PodName,
		item.ContainerName,
		item.NodeName,
		string(explain.Analyze(item.Memory).Diagnosis),
	}
	return containsAny(values, query)
}

func containsAny(values []string, query string) bool {
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}
