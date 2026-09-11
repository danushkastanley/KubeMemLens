package observationview

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"github.com/danushkastanley/kube-memlens/internal/observation"
	"github.com/danushkastanley/kube-memlens/internal/resourceview"
	"k8s.io/apimachinery/pkg/util/validation"
)

func Detail(row Row, now time.Time) []string {
	lines := Summary(row, now)
	if row.Pod != nil {
		pod := row.Pod
		owner := "unreported"
		if pod.Context.OwnerName != "" {
			owner = pod.Context.OwnerKind + "/" + pod.Context.OwnerName
		} else if pod.OwnerAvailability == capability.Available {
			owner = "none"
		}
		lines = append(lines, "", "Kubernetes status:", "Pod phase: "+textOrUnreported(pod.Context.Phase), "Pod age: "+Age(pod.Context.CreatedAt, now),
			"Node: "+textOrUnreported(pod.NodeName), "Owner: "+owner,
			"Owner lookup: "+string(pod.OwnerAvailability))
		if pod.OwnerReason != "" {
			lines = append(lines, "Owner reason: "+string(pod.OwnerReason))
		}
		lines = append(lines, "Configured Pod request: "+resourceBytes(pod.Context.Resources.Configured.Request), "Configured Pod limit: "+resourceBytes(pod.Context.Resources.Configured.Limit))
		lines = append(lines, "QoS: "+textOrUnreported(pod.Context.QoSClass))
		lines = append(lines, resourceview.PodContextLines(pod.Context, len(pod.Containers))...)
		lines = append(lines, hugepageLines(pod.Hugepages)...)
	}
	if row.Container != nil {
		lines = append(lines, containerLines(*row.Container)...)
	} else if row.Pod != nil {
		for _, container := range row.Pod.Containers {
			lines = append(lines, containerLines(container)...)
		}
	}
	if row.Node != nil {
		node := row.Node
		lines = append(lines, "Node age: "+Age(node.CreatedAt, now))
		lines = append(lines, "", "Kubernetes Node status:", "Availability: "+string(node.StatusAvailability), "MemoryPressure: "+textOrUnreported(node.MemoryPressure),
			"Capacity: "+optionalBytes(node.CapacityMemoryBytes), "Allocatable: "+optionalBytes(node.AllocatableMemoryBytes), "Node working set is independent of summed visible Pod working sets.")
		lines = append(lines, nodeHugepages("capacity", node.HugepageCapacity)...)
		lines = append(lines, nodeHugepages("allocatable", node.HugepageAllocatable)...)
		if node.StatusReason != "" {
			lines = append(lines, "Status reason: "+string(node.StatusReason))
		}
	}
	if row.Count > 0 {
		lines = append(lines, fmt.Sprintf("Visible Pods: %d", row.Count))
	}
	return lines
}

func containerLines(container observation.Container) []string {
	context := container.Context
	lines := []string{"", "Container: " + container.Name + " (" + container.Kind + ")", "State: " + textOrUnreported(container.State),
		"Configured request: " + resourceBytes(model.ResourceValue{Bytes: context.MemoryRequestBytes, Known: context.MemoryRequestKnown}),
		"Configured limit: " + resourceBytes(model.ResourceValue{Bytes: context.MemoryLimitBytes, Known: context.MemoryLimitKnown})}
	if resources := resourceview.ContainerContextLines(context); len(resources) > 0 {
		lines = append(lines, resources[2:]...)
	}
	if container.ExitCode != nil {
		lines = append(lines, fmt.Sprintf("Current exit code: %d", *container.ExitCode))
	}
	if container.StateReason != "" {
		lines = append(lines, "Current state reason: "+container.StateReason)
	}
	if container.State != "unreported" {
		lines = append(lines, fmt.Sprintf("Reported restarts: %d", context.RestartCount))
	}
	if context.LastTerminationKnown {
		lines = append(lines, fmt.Sprintf("Previous termination: %s (exit %d)", context.LastTerminationReason, context.LastTerminationExitCode))
	}
	return append(lines, hugepageLines(container.Hugepages)...)
}

func hugepageLines(values []observation.Hugepages) []string {
	var lines []string
	for _, value := range values {
		lines = append(lines, value.Resource+" request: "+optionalBytes(value.RequestBytes)+"; limit: "+optionalBytes(value.LimitBytes))
	}
	return lines
}

func nodeHugepages(label string, values map[string]uint64) []string {
	if len(values) == 0 {
		return []string{"Hugepage " + label + ": unreported"}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, key+" "+label+": "+model.FormatBytes(values[key]))
	}
	return lines
}

func optionalBytes(value *uint64) string {
	if value == nil {
		return "unreported"
	}
	return model.FormatBytes(*value)
}

func resourceBytes(value model.ResourceValue) string {
	if !value.Known {
		return "not set"
	}
	return model.FormatBytes(value.Bytes)
}

// Recommendations suggest investigation, never a limit change based on absent
// composition, pressure or event evidence.
func Recommendations(row Row) []string {
	lines := []string{"Check configured resources and Kubernetes status before changing memory limits.", "Working set alone does not establish OOM risk, a leak or reclaim behaviour.", "Keep the current kubeconfig and context when running follow-up commands."}
	if row.WorkingSet == nil || row.WorkingSet.Bytes == nil {
		lines = append(lines, "Check Metrics API availability and read permissions for the requested scope.")
	}
	if command, ok := Command(row); ok {
		lines = append(lines, "Read-only follow-up: "+command)
	}
	return lines
}

func Command(row Row) (string, bool) {
	if row.Mode != capability.Deep && row.Mode != capability.Restricted {
		return "", false
	}
	prefix := "kubectl memlens --mode=" + string(row.Mode)

	if len(validation.IsDNS1123Subdomain(row.Name)) != 0 {
		return "", false
	}
	if row.Scope == capability.NodeScope {
		return "kubectl describe node " + row.Name, true
	}
	if len(validation.IsDNS1123Label(row.Namespace)) != 0 {
		return "", false
	}
	switch row.Scope {
	case capability.PodScope, capability.ContainerScope:
		if len(validation.IsDNS1123Subdomain(row.PodName)) != 0 {
			return "", false
		}
		return prefix + " explain pod " + row.PodName + " -n " + row.Namespace, true
	case capability.NamespaceScope:
		return prefix + " top pods -n " + row.Namespace, true
	case capability.WorkloadScope:
		kind := strings.ToLower(row.Kind)
		if len(validation.IsDNS1035Label(kind)) != 0 {
			return "", false
		}
		return prefix + " explain workload " + kind + "/" + row.Name + " -n " + row.Namespace, true
	default:
		return "", false
	}
}
