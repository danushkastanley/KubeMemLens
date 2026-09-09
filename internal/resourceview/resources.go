// Package resourceview presents Kubernetes resource evidence consistently in
// the CLI and terminal interface, independently from cgroup measurements.
package resourceview

import (
	"fmt"
	"strconv"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func Effective(pod api.PodSnapshot) model.EffectiveMemoryResources {
	return model.EffectivePodMemory(pod.Context.Resources, model.ContainerResourceTotals{
		RequestBytes: pod.Context.MemoryRequestBytes, LimitBytes: pod.Context.MemoryLimitBytes,
		RequestContainers: pod.Context.MemoryRequestContainers, LimitContainers: pod.Context.MemoryLimitContainers,
		Containers: len(pod.Containers),
	})
}

func PodLines(pod api.PodSnapshot) []string {
	resources := pod.Context.Resources
	if resources.IsZero() {
		return nil
	}
	var lines []string
	if resources.Configured.Request.Known || resources.Configured.Limit.Known {
		effective := Effective(pod)
		lines = append(lines,
			"Effective request:     "+effectiveValue(effective.Request),
			"Effective limit:       "+effectiveValue(effective.Limit),
			"Kubelet allocated Pod request: "+resourceValue(resources.AllocatedRequest, "not reported"),
			"Kubelet applied Pod request:   "+resourceValue(resources.Applied.Request, "not reported"),
			"Kubelet applied Pod limit:     "+resourceValue(resources.Applied.Limit, "not reported"),
		)
	}
	lines = append(lines, resizeLines(resources)...)
	if resources.Generation > 0 {
		observed := "not reported"
		if resources.ObservedGeneration > 0 {
			observed = strconv.FormatInt(resources.ObservedGeneration, 10)
		}
		lines = append(lines, fmt.Sprintf("Pod spec generation: %d; kubelet observed: %s", resources.Generation, observed))
	}
	for _, container := range pod.Containers {
		for _, line := range ContainerLines(container) {
			lines = append(lines, "Container "+container.ContainerName+" — "+line)
		}
	}
	return lines
}

// ConfiguredContainer keeps the existing per-container specification separate
// from kubelet allocations and the effective Pod budget.
func ConfiguredContainer(container api.ContainerSnapshot) model.MemoryResourceBudget {
	return model.MemoryResourceBudget{
		Request: model.ResourceValue{Known: container.Context.MemoryRequestKnown, Bytes: container.Context.MemoryRequestBytes},
		Limit:   model.ResourceValue{Known: container.Context.MemoryLimitKnown, Bytes: container.Context.MemoryLimitBytes},
	}
}

func ContainerLines(container api.ContainerSnapshot) []string {
	resources := container.Context.Resources
	if resources.IsZero() {
		return nil
	}
	configured := ConfiguredContainer(container)
	return []string{
		"Configured request: " + resourceValue(configured.Request, "not set"),
		"Configured limit:   " + resourceValue(configured.Limit, "not set"),
		"Kubelet allocated request: " + resourceValue(resources.AllocatedRequest, "not reported"),
		"Kubelet applied request:   " + resourceValue(resources.Applied.Request, "not reported"),
		"Kubelet applied limit:     " + resourceValue(resources.Applied.Limit, "not reported"),
	}
}

func resizeLines(resources model.PodMemoryResources) []string {
	var lines []string
	for _, entry := range []struct {
		label       string
		observation model.ResizeObservation
	}{{"Resize allocation", resources.Pending}, {"Resize application", resources.Applying}} {
		if entry.observation.State == model.ResizeNone {
			continue
		}
		label := entry.label
		if entry.observation.Source == model.ResizeLegacyStatus {
			label = "Resize (legacy status)"
		}
		line := label + ": " + resizeState(entry.observation.State)
		if entry.observation.ObservedGeneration > 0 {
			line += fmt.Sprintf(" (generation %d)", entry.observation.ObservedGeneration)
		}
		lines = append(lines, line)
	}
	return lines
}

func resizeState(state model.ResizeState) string {
	switch state {
	case model.ResizeNone:
		return "not reported"
	case model.ResizePending, model.ResizeDeferred, model.ResizeInfeasible, model.ResizeApplying, model.ResizeError:
		return string(state)
	default:
		return "unknown"
	}
}

func resourceValue(value model.ResourceValue, absent string) string {
	if !value.Known {
		return absent
	}
	return model.FormatCompactBytes(value.Bytes)
}

func effectiveValue(value model.EffectiveResourceValue) string {
	switch value.Source {
	case model.BudgetPod:
		return model.FormatCompactBytes(value.Bytes) + " (Pod configuration)"
	case model.BudgetContainers:
		return model.FormatCompactBytes(value.Bytes) + " (container sum)"
	case model.BudgetPartial:
		return model.FormatCompactBytes(value.Bytes) + " (partial container sum)"
	case model.BudgetUnset:
		return "not set"
	default:
		return "not reported"
	}
}
