package agentless

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/observation"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

func (r *Reader) readNodes(ctx context.Context, pods []observation.Pod, at time.Time) ([]observation.Node, observation.SourceReport) {
	report := statusReport(capability.NodeScope)
	if r.namespace == "" {
		nodes, err := r.listNodes(ctx, at)
		if err != nil {
			report.Availability, report.Reason = fieldFailure(err)
			report.Completeness = capability.Partial
			return nil, report
		}
		return nodes, report
	}
	names := map[string]bool{}
	for i := range pods {
		if pods[i].NodeName != "" {
			names[pods[i].NodeName] = true
		}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	if len(ordered) == 0 {
		return nil, unqueriedReport(capability.KubernetesStatus, capability.NodeScope)
	}
	if len(ordered) > r.opts.MaxNodes {
		ordered = ordered[:r.opts.MaxNodes]
		report.Completeness, report.Reason = capability.Partial, limitReached
	}
	nodes := make([]observation.Node, 0, len(ordered))
	for _, name := range ordered {
		node := unavailableNode(name, at, capability.Unreported, capability.NotObserved)
		object, err := r.client.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			node, err = nodeMetadata(*object, r.opts.Now().UTC())
			if object.Name != name {
				err = queryFailure(capability.InvalidResponse, nil)
			}
		}
		if err != nil {
			availability, reason := fieldFailure(err)
			node = unavailableNode(name, at, availability, reason)
			report.Completeness, report.Reason = capability.Partial, reason
		}
		nodes = append(nodes, node)
	}
	available := 0
	for _, node := range nodes {
		if node.StatusAvailability == capability.Available {
			available++
		}
	}
	if len(nodes) > 0 && available == 0 {
		report.Availability, report.Freshness = nodes[0].StatusAvailability, capability.UnknownFreshness
	}
	return nodes, report
}

func (r *Reader) listNodes(ctx context.Context, at time.Time) ([]observation.Node, error) {
	nodes := []observation.Node{}
	seen, cursors := map[string]bool{}, map[string]bool{}
	cursor, version := "", ""
	for page := 0; page < r.opts.MaxPages; page++ {
		list, err := r.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: int64(r.opts.PageSize), Continue: cursor})
		if err != nil {
			return nil, readFailure(err)
		}
		if page > 0 && list.ResourceVersion != version {
			return nil, queryFailure(capability.InvalidResponse, nil)
		}
		version = list.ResourceVersion
		for _, object := range list.Items {
			if seen[object.Name] {
				return nil, queryFailure(capability.InvalidResponse, nil)
			}
			if len(nodes) >= r.opts.MaxNodes {
				return nil, queryFailure(limitReached, nil)
			}
			node, err := nodeMetadata(object, r.opts.Now().UTC())
			if err != nil {
				return nil, err
			}
			seen[node.Name] = true
			nodes = append(nodes, node)
		}
		cursor = list.Continue
		if cursor == "" {
			sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
			return nodes, nil
		}
		if len(cursor) > 4096 || cursors[cursor] || len(list.Items) == 0 {
			return nil, queryFailure(capability.InvalidResponse, nil)
		}
		cursors[cursor] = true
	}
	return nil, queryFailure(limitReached, nil)
}

func unavailableNode(name string, at time.Time, availability capability.Availability, reason capability.Reason) observation.Node {
	evidence := statusEnvelope(at, capability.NodeScope)
	evidence.Completeness, evidence.Freshness = capability.Partial, capability.UnknownFreshness
	return observation.Node{Name: name, StatusAvailability: availability, StatusReason: reason, StatusEvidence: evidence, MemoryPressure: "unreported"}
}

func nodeMetadata(node corev1.Node, at time.Time) (observation.Node, error) {
	if len(validation.IsDNS1123Subdomain(node.Name)) != 0 || node.Namespace != "" || node.UID == "" || len(node.UID) > 128 {
		return observation.Node{}, queryFailure(capability.InvalidResponse, nil)
	}
	result := observation.Node{Name: node.Name, UID: string(node.UID), CreatedAt: node.CreationTimestamp.Time,
		StatusAvailability: capability.Available, StatusEvidence: statusEnvelope(at, capability.NodeScope), MemoryPressure: "unreported"}
	for _, pair := range []struct {
		resources corev1.ResourceList
		memory    **uint64
		hugepages *map[string]uint64
	}{
		{node.Status.Capacity, &result.CapacityMemoryBytes, &result.HugepageCapacity},
		{node.Status.Allocatable, &result.AllocatableMemoryBytes, &result.HugepageAllocatable},
	} {
		for name, quantity := range pair.resources {
			if name != corev1.ResourceMemory && !strings.HasPrefix(string(name), corev1.ResourceHugePagesPrefix) {
				continue
			}
			if quantity.Sign() < 0 || quantity.Cmp(*resource.NewQuantity(math.MaxInt64, resource.DecimalSI)) > 0 {
				return observation.Node{}, queryFailure(capability.InvalidResponse, nil)
			}
			bytes := uint64(quantity.Value())
			if name == corev1.ResourceMemory {
				*pair.memory = &bytes
				continue
			}
			if *pair.hugepages == nil {
				*pair.hugepages = map[string]uint64{}
			}
			(*pair.hugepages)[string(name)] = bytes
		}
	}
	seen := false
	for _, condition := range node.Status.Conditions {
		if condition.Type != corev1.NodeMemoryPressure {
			continue
		}
		if seen {
			return observation.Node{}, queryFailure(capability.InvalidResponse, nil)
		}
		seen = true
		switch condition.Status {
		case corev1.ConditionTrue:
			result.MemoryPressure = "adverse"
		case corev1.ConditionFalse:
			result.MemoryPressure = "healthy"
		case corev1.ConditionUnknown:
			result.MemoryPressure = "unknown"
		default:
			return observation.Node{}, queryFailure(capability.InvalidResponse, nil)
		}
	}
	return result, nil
}
