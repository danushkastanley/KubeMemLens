package tui

import (
	"strings"

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
	query = strings.ToLower(strings.TrimSpace(query))
	filtered := make([]api.PodSnapshot, 0, len(items))
	for _, item := range items {
		if !allNamespaces && namespace != "" && item.Namespace != namespace {
			continue
		}
		if query != "" && !podMatches(item, query) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func FilterWorkloads(items []api.WorkloadSnapshot, namespace string, allNamespaces bool, query string) []api.WorkloadSnapshot {
	query = strings.ToLower(strings.TrimSpace(query))
	filtered := make([]api.WorkloadSnapshot, 0, len(items))
	for _, item := range items {
		if !allNamespaces && namespace != "" && item.Namespace != namespace {
			continue
		}
		if query != "" && !containsAny([]string{item.Namespace, item.Kind, item.Name, item.LargestPodName, string(explain.Analyze(item.Memory).Diagnosis)}, query) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func FilterContainers(items []api.ContainerSnapshot, namespace string, allNamespaces bool, query string) []api.ContainerSnapshot {
	query = strings.ToLower(strings.TrimSpace(query))
	filtered := make([]api.ContainerSnapshot, 0, len(items))
	for _, item := range items {
		if item.Namespace == "" || item.PodName == "" || item.ContainerName == "" {
			continue
		}
		if !allNamespaces && namespace != "" && item.Namespace != namespace {
			continue
		}
		if query != "" && !containerMatches(item, query) {
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
