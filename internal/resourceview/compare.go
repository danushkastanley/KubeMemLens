package resourceview

import (
	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"sort"
)

func ComparisonLines(before, after api.PodSnapshot) []string {
	left, right := before.Context.Resources, after.Context.Resources
	var lines []string
	for _, value := range []struct {
		name          string
		before, after model.ResourceValue
	}{
		{"Pod configured request", left.Configured.Request, right.Configured.Request},
		{"Pod configured limit", left.Configured.Limit, right.Configured.Limit},
		{"Pod allocated request", left.AllocatedRequest, right.AllocatedRequest},
		{"Pod applied request", left.Applied.Request, right.Applied.Request},
		{"Pod applied limit", left.Applied.Limit, right.Applied.Limit},
	} {
		if value.before != value.after {
			lines = append(lines, value.name+": "+resourceValue(value.before, "not reported")+" -> "+resourceValue(value.after, "not reported"))
		}
	}
	if left.Pending.State != right.Pending.State {
		lines = append(lines, "Resize allocation: "+resizeState(left.Pending.State)+" -> "+resizeState(right.Pending.State))
	}
	if left.Applying.State != right.Applying.State {
		lines = append(lines, "Resize application: "+resizeState(left.Applying.State)+" -> "+resizeState(right.Applying.State))
	}
	lines = append(lines, containerComparisonLines(before.Containers, after.Containers)...)
	return lines
}

func containerComparisonLines(before, after []api.ContainerSnapshot) []string {
	left, right := map[string]api.ContainerSnapshot{}, map[string]api.ContainerSnapshot{}
	names := map[string]bool{}
	for _, container := range before {
		left[container.ContainerName] = container
		names[container.ContainerName] = true
	}
	for _, container := range after {
		right[container.ContainerName] = container
		names[container.ContainerName] = true
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	var lines []string
	for _, name := range ordered {
		a, b := left[name], right[name]
		if a.Context.Resources.IsZero() && b.Context.Resources.IsZero() {
			continue
		}
		configuredA, configuredB := ConfiguredContainer(a), ConfiguredContainer(b)
		for _, value := range []struct {
			name          string
			before, after model.ResourceValue
		}{
			{"configured request", configuredA.Request, configuredB.Request},
			{"configured limit", configuredA.Limit, configuredB.Limit},
			{"allocated request", a.Context.Resources.AllocatedRequest, b.Context.Resources.AllocatedRequest},
			{"applied request", a.Context.Resources.Applied.Request, b.Context.Resources.Applied.Request},
			{"applied limit", a.Context.Resources.Applied.Limit, b.Context.Resources.Applied.Limit},
		} {
			if value.before != value.after {
				lines = append(lines, "Container "+name+" "+value.name+": "+resourceValue(value.before, "not reported")+" -> "+resourceValue(value.after, "not reported"))
			}
		}
	}
	return lines
}
