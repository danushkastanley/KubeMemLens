package tui

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

// podListKey names everything the visible Pod list depends on. The snapshot is
// identified by its backing array: a refresh replaces the slice and nothing
// edits Pods in place.
type podListKey struct {
	first        *api.PodSnapshot
	count        int
	namespace    string
	allNamespace bool
	node         string
	workloadKind string
	workloadName string
	query        string
	sort         sortMode
	now          time.Time
	staleAfter   time.Duration
}

// podListCache keeps the last filtered and sorted Pod list. One keypress asks
// for the list several times, and each used to filter and risk-sort every Pod
// again. The model is copied by value, so every copy shares this pointer.
// Callers must not modify the returned slice.
type podListCache struct {
	key   podListKey
	items []api.PodSnapshot
	valid bool
}

func (c *podListCache) get(key podListKey, build func() []api.PodSnapshot) []api.PodSnapshot {
	if c == nil {
		return build()
	}
	if !c.valid || c.key != key {
		c.key, c.items, c.valid = key, build(), true
	}
	return c.items
}

func (m appModel) podListKey() podListKey {
	namespace, all := m.activeNamespace()
	key := podListKey{count: len(m.data.Pods), namespace: namespace, allNamespace: all, node: m.currentNode,
		workloadKind: m.currentWorkloadKind, workloadName: m.currentWorkloadName, query: m.query, sort: m.sort,
		now: m.riskNow(), staleAfter: m.staleAfter()}
	if len(m.data.Pods) > 0 {
		key.first = &m.data.Pods[0]
	}
	return key
}
