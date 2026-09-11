// Package observationview projects current evidence for CLI and TUI views.
package observationview

import (
	"sort"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"github.com/danushkastanley/kube-memlens/internal/observation"
)

type Row struct {
	Mode                capability.Mode                 `json:"mode"`
	Scope               capability.Scope                `json:"scope"`
	Namespace           string                          `json:"namespace,omitempty"`
	Name                string                          `json:"name"`
	Kind                string                          `json:"kind"`
	PodName             string                          `json:"pod,omitempty"`
	NodeName            string                          `json:"node,omitempty"`
	WorkloadKind        string                          `json:"workloadKind,omitempty"`
	WorkloadName        string                          `json:"workloadName,omitempty"`
	Count               int                             `json:"count,omitempty"`
	Phase               string                          `json:"phase,omitempty"`
	CreatedAt           time.Time                       `json:"createdAt,omitzero"`
	WorkingSet          *observation.WorkingSet         `json:"workingSet,omitempty"`
	Cgroup              *observation.Cgroup             `json:"cgroup,omitempty"`
	ConfiguredResources *model.MemoryResourceBudget     `json:"configuredResources,omitempty"`
	PodResources        *model.PodMemoryResources       `json:"podResources,omitempty"`
	ContainerResources  *model.ContainerMemoryResources `json:"containerResources,omitempty"`
	ContainerState      string                          `json:"containerState,omitempty"`
	NodeMemoryPressure  string                          `json:"nodeMemoryPressure,omitempty"`
	Labels              map[string]string               `json:"-"`
	Pod                 *observation.Pod                `json:"-"`
	Container           *observation.Container          `json:"-"`
	Node                *observation.Node               `json:"-"`
}

func Rows(batch observation.Batch) []Row {
	rows := make([]Row, 0, len(batch.Pods)+len(batch.Nodes)+len(batch.Namespaces)+len(batch.Workloads))
	for i := range batch.Nodes {
		node := &batch.Nodes[i]
		row := Row{Mode: batch.Mode, Scope: capability.NodeScope, Name: node.Name, Kind: "Node", NodeName: node.Name, CreatedAt: node.CreatedAt, NodeMemoryPressure: node.MemoryPressure, Node: node}
		if batch.Mode == capability.Restricted {
			row.WorkingSet = &node.WorkingSet
		}
		rows = append(rows, row)
	}
	for _, entry := range []struct {
		scope  capability.Scope
		groups []observation.Group
	}{{capability.NamespaceScope, batch.Namespaces}, {capability.WorkloadScope, batch.Workloads}} {
		for _, group := range entry.groups {
			row := Row{Mode: batch.Mode, Scope: entry.scope, Namespace: group.Namespace, Name: group.Name, Kind: group.Kind, Count: group.PodCount, Cgroup: group.Cgroup}
			if entry.scope == capability.NamespaceScope {
				row.Namespace = group.Name
			}
			if entry.scope == capability.WorkloadScope {
				row.WorkloadKind, row.WorkloadName = group.Kind, group.Name
			}
			if batch.Mode == capability.Restricted {
				value := group.WorkingSet
				row.WorkingSet = &value
				row.Cgroup = nil
			}
			rows = append(rows, row)
		}
	}
	for i := range batch.Pods {
		pod := &batch.Pods[i]
		row := Row{Mode: batch.Mode, Scope: capability.PodScope, Namespace: pod.Namespace, Name: pod.Name, Kind: "Pod", PodName: pod.Name, NodeName: pod.NodeName,
			WorkloadKind: pod.Context.WorkloadKind, WorkloadName: pod.Context.WorkloadName, Phase: pod.Context.Phase, CreatedAt: pod.Context.CreatedAt, Labels: pod.Context.Labels, Pod: pod, Cgroup: pod.Cgroup}
		if batch.Mode == capability.Restricted {
			row.WorkingSet = &pod.WorkingSet
			row.Cgroup = nil
			row.ConfiguredResources = configuredBudget(pod.Context.Resources.Configured)
			if !pod.Context.Resources.IsZero() {
				row.PodResources = &pod.Context.Resources
			}
			if row.WorkloadName == "" {
				row.WorkloadKind, row.WorkloadName = "Pod", pod.Name
			}
		}
		rows = append(rows, row)
		for c := range pod.Containers {
			container := &pod.Containers[c]
			child := row
			child.Scope, child.Name, child.Kind = capability.ContainerScope, container.Name, "Container"
			child.Container, child.Cgroup = container, container.Cgroup
			if batch.Mode == capability.Restricted {
				child.WorkingSet = &container.WorkingSet
				child.Cgroup, child.PodResources = nil, nil
				child.ContainerState = container.State
				if !container.Context.Resources.IsZero() {
					child.ContainerResources = &container.Context.Resources
				}
				child.ConfiguredResources = configuredBudget(model.MemoryResourceBudget{
					Request: model.ResourceValue{Bytes: container.Context.MemoryRequestBytes, Known: container.Context.MemoryRequestKnown},
					Limit:   model.ResourceValue{Bytes: container.Context.MemoryLimitBytes, Known: container.Context.MemoryLimitKnown},
				})
			}
			rows = append(rows, child)
		}
	}
	return rows
}

func configuredBudget(value model.MemoryResourceBudget) *model.MemoryResourceBudget {
	if !value.Request.Known && !value.Limit.Known {
		return nil
	}
	return &value
}

func (r Row) Key() string {
	switch r.Scope {
	case capability.NodeScope:
		return "node/" + r.Name
	case capability.NamespaceScope:
		return "namespace/" + r.Namespace
	case capability.WorkloadScope:
		return "workload/" + r.Namespace + "/" + r.Kind + "/" + r.Name
	case capability.ContainerScope:
		return "container/" + r.Namespace + "/" + r.PodName + "/" + r.Name
	default:
		return "pod/" + r.Namespace + "/" + r.Name
	}
}

type Order string

const (
	ByMemory    Order = "memory"
	ByName      Order = "name"
	ByNamespace Order = "namespace"
)

// Sort uses only the row's actual measurement. Missing values sort last;
// identity provides a deterministic tie break, including measured zeros.
func Sort(rows []Row, order Order) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		switch order {
		case ByName:
			if a.Name != b.Name {
				return a.Name < b.Name
			}
		case ByNamespace:
			if a.Namespace != b.Namespace {
				return a.Namespace < b.Namespace
			}
		default:
			left, right := a.Bytes(), b.Bytes()
			if left == nil || right == nil {
				if left != right {
					return right == nil
				}
			} else if *left != *right {
				return *left > *right
			}
		}
		return a.Key() < b.Key()
	})
}

func (r Row) Bytes() *uint64 {
	if r.Cgroup != nil {
		return &r.Cgroup.Memory.TotalBytes
	}
	if r.WorkingSet != nil {
		return r.WorkingSet.Bytes
	}
	return nil
}

func (r Row) Matches(query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	for _, value := range []string{r.Namespace, r.Name, r.Kind, r.PodName, r.NodeName, r.WorkloadKind, r.WorkloadName, r.Phase, r.ContainerState, r.NodeMemoryPressure} {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}
