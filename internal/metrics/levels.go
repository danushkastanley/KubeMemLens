package metrics

import "github.com/danushkastanley/kube-memlens/internal/api"

const (
	eofLine = "# EOF\n"

	droppedDisabled       = "disabled"
	droppedMaxEntities    = "max_entities_exceeded"
	droppedResponseBudget = "response_budget_exceeded"

	// Room kept for the dropped-entities family, which is written before the
	// entity levels but only known once each level has been sized.
	droppedFamilyReserve = 1024
)

// entityLevel is one optional group of per-entity series. Collector state,
// freshness and ingestion series are never optional: alerting depends on them,
// so entity levels give way first when the response budget is tight.
type entityLevel struct {
	name    string
	count   int
	dropped string
	render  func(*renderer)
}

func entityLevels(namespaces []api.NamespaceSnapshot, pods []api.PodSnapshot, containers []api.ContainerSnapshot, namespaceCount, podCount, containerCount int, opts Options) []entityLevel {
	levels := make([]entityLevel, 0, 3)
	if opts.IncludeNamespaceMetrics {
		level := entityLevel{name: "namespace", count: namespaceCount, render: func(r *renderer) { r.renderNamespaces(namespaces, opts) }}
		if namespaceCount > len(namespaces) {
			level.dropped = droppedResponseBudget
		}
		levels = append(levels, level)
	}
	return append(levels,
		entityLevel{
			name: "pod", count: podCount, dropped: cardinalityGuardrail(opts.IncludePodMetrics, podCount, opts.MaxPods),
			render: func(r *renderer) { r.renderPods(pods, opts) },
		},
		entityLevel{
			name: "container", count: containerCount, dropped: cardinalityGuardrail(opts.IncludeContainerMetrics, containerCount, opts.MaxContainers),
			render: func(r *renderer) { r.renderContainers(containers, opts) },
		},
	)
}

func cardinalityGuardrail(enabled bool, count, maximum int) string {
	if !enabled {
		return droppedDisabled
	}
	if count > maximum {
		return droppedMaxEntities
	}
	return ""
}

// renderLevels writes the dropped-entities family, then each level that fits
// in what is left of the response budget after reservedTail bytes. Coarser
// levels are sized first, so the most detailed series are the first to go.
func (r *renderer) renderLevels(levels []entityLevel, reservedTail int) {
	used := r.b.Len() + reservedTail + droppedFamilyReserve
	sections := make([]string, 0, len(levels))
	for index := range levels {
		level := &levels[index]
		if level.dropped != "" {
			continue
		}
		section, fits := r.section(used, level.render)
		if !fits {
			level.dropped = droppedResponseBudget
			continue
		}
		used += len(section)
		sections = append(sections, section)
	}

	r.helpType("kubememlens_metrics_dropped_entities", "KubeMemLens entities skipped by metrics cardinality guardrails.", "gauge")
	for _, level := range levels {
		if level.dropped != "" {
			r.sample("kubememlens_metrics_dropped_entities", labels{"level": level.name, "reason": level.dropped}, float64(level.count))
		}
	}
	for _, section := range sections {
		_, _ = r.b.WriteString(section)
	}
}

// section renders one level on its own so that it can be left out whole.
func (r *renderer) section(used int, render func(*renderer)) (string, bool) {
	remaining := 0
	if r.b.max > 0 {
		remaining = r.b.max - used
		if remaining <= 0 {
			return "", false
		}
	}
	section := newRenderer(remaining)
	render(section)
	return section.b.String(), section.b.err == nil
}
