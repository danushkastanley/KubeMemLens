package capability

// Plan is pure. A source can be accessible while its observations are stale,
// partial or not yet sampled. Those conditions must not change memory meaning.
func Plan(request Mode, sources []SourceState) (Selection, error) {
	mode, err := ParseMode(string(request))
	result := Selection{State: Unavailable, Freshness: UnknownFreshness, Completeness: Partial,
		Sources: append([]SourceState(nil), sources...)}
	if err != nil {
		return result, err
	}
	deep := findSource(sources, Cgroup)
	status := findSource(sources, KubernetesStatus)
	metrics := findSource(sources, KubernetesMetrics)
	selected := deep
	switch {
	case mode == Deep || mode == Auto && deep.Availability == Available:
		result.Mode = Deep
	case mode == Restricted || mode == Auto && fallbackAllowed(deep):
		result.Mode, selected = Restricted, status
	default:
		result.Mode = Deep
	}
	if selected.Availability != Available {
		result.Queries = unavailableQueries(selected.Reason)
		return result, &SelectionError{Mode: result.Mode, Reason: selected.Reason}
	}
	result.State, result.Freshness, result.Completeness = Available, selected.Freshness, selected.Completeness
	if result.Mode == Restricted {
		result.Freshness, result.Completeness = metrics.Freshness, Partial
		result.Queries = append(result.Queries, QueryState{Query: Current, Availability: Available}, QueryState{Query: Capture, Availability: Available})
		for _, query := range []Query{VolumeHealth, NodeContext, Trace} {
			result.Queries = append(result.Queries, QueryState{Query: query, Availability: Unavailable, Reason: QueryNotImplemented})
		}
		for _, query := range []Query{Composition, LocalEvents, Pressure, History} {
			result.Queries = append(result.Queries, QueryState{Query: query, Availability: Unavailable, Reason: RequiresDeep})
		}
		return result, nil
	}
	for _, query := range []Query{Current, Composition, LocalEvents, Pressure, History, Capture} {
		result.Queries = append(result.Queries, QueryState{Query: query, Availability: Available})
	}
	for _, query := range []Query{VolumeHealth, NodeContext, Trace} {
		result.Queries = append(result.Queries, QueryState{Query: query, Availability: Unsupported, Reason: QueryNotImplemented})
	}
	return result, nil
}

func findSource(sources []SourceState, source Source) SourceState {
	for _, state := range sources {
		if state.Source == source {
			return state
		}
	}
	return SourceState{Source: source, Availability: Unavailable, Reason: NotObserved,
		Freshness: UnknownFreshness, Completeness: Partial}
}

func fallbackAllowed(deep SourceState) bool {
	return deep.Availability == Absent || deep.Availability == Forbidden && deep.Reason == AccessDenied
}

func unavailableQueries(reason Reason) []QueryState {
	var queries []QueryState
	for _, query := range []Query{Current, Composition, LocalEvents, Pressure, History, Capture, VolumeHealth, NodeContext, Trace} {
		queries = append(queries, QueryState{Query: query, Availability: Unavailable, Reason: reason})
	}
	return queries
}
