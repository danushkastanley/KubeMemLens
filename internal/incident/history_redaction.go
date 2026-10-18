package incident

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

type historyAliases struct {
	values map[string]string
	counts map[string]int
}

func (a *historyAliases) name(kind, key string) string {
	if key == "" {
		return ""
	}
	combined := kind + "\x00" + key
	if prior, ok := a.values[combined]; ok {
		return prior
	}
	a.counts[kind]++
	value := kind + "-" + strconv.Itoa(a.counts[kind])
	a.values[combined] = value
	return value
}
func (a *historyAliases) object(o changemarkers.Object) changemarkers.Object {
	o.Name = a.name(strings.ToLower(o.Kind), o.Namespace+"/"+o.Name)
	o.Namespace = a.name("namespace", o.Namespace)
	o.UID = a.name("uid", o.UID)
	return o
}
func (a *historyAliases) target(t memoryhistory.Target) memoryhistory.Target {
	if t.Container != "" {
		t.Container = a.name("container", t.PodUID+"/"+t.Container)
	}
	t.ContainerID = a.name("container-id", t.ContainerID)
	t.Pod = a.name("pod", t.Namespace+"/"+t.Pod)
	t.Namespace = a.name("namespace", t.Namespace)
	t.PodUID = a.name("uid", t.PodUID)
	t.Node = a.name("node", t.Node)
	t.NodeUID = a.name("uid", t.NodeUID)
	return t
}
func redactHistory(b *HistoryBundle) error {
	a := &historyAliases{values: map[string]string{}, counts: map[string]int{}}
	c := &b.Context
	s := &c.History.Selection
	request := s.Request
	kind := "pod"
	if request.Scope == memoryhistory.Workload {
		kind = strings.ToLower(request.WorkloadKind)
	}
	request.Name = a.name(kind, request.Namespace+"/"+request.Name)
	request.Namespace = a.name("namespace", request.Namespace)
	if request.Container != "" {
		request.Container = a.name("container", s.Targets[0].PodUID+"/"+request.Container)
	}
	s.Request = request
	s.UID = a.name("uid", s.UID)
	s.Revision = ""
	for i := range s.Targets {
		s.Targets[i] = a.target(s.Targets[i])
	}
	for i := range c.History.Series {
		c.History.Series[i].Target = a.target(c.History.Series[i].Target)
	}
	c.Changes.Request = request
	c.Changes.UID = s.UID
	for i := range c.Changes.Markers {
		m := &c.Changes.Markers[i]
		if m.Container != "" {
			m.Container = a.name("container", m.Subject.UID+"/"+m.Container)
		}
		m.Subject = a.object(m.Subject)
		m.SourceUID = a.name("uid", m.SourceUID)
		m.PreviousUID = a.name("uid", m.PreviousUID)
		for j := range m.Owners {
			m.Owners[j] = a.object(m.Owners[j])
		}
	}
	b.Redacted = true
	report, err := changemarkers.Compose(c.History.Selection, c.History.Query, c.Changes.ObservedAt, c.Changes.Events, c.Changes.Markers, c.Changes.Truncated)
	if err != nil {
		return err
	}
	c.Changes = report
	return nil
}

func alias(value, prefix string) bool {
	suffix, ok := strings.CutPrefix(value, prefix+"-")
	if !ok || suffix == "" || suffix[0] == '0' {
		return false
	}
	n, err := strconv.Atoi(suffix)
	return err == nil && n > 0 && n <= 4096 && strconv.Itoa(n) == suffix
}
func validateHistoryAliases(c changemarkers.Context) error {
	invalid := fmt.Errorf("redacted history incident contains raw identity")
	r := c.History.Selection.Request
	kind := "pod"
	if r.Scope == memoryhistory.Workload {
		kind = strings.ToLower(r.WorkloadKind)
	}
	if !alias(r.Namespace, "namespace") || !alias(r.Name, kind) || (r.Container != "" && !alias(r.Container, "container")) || !alias(c.History.Selection.UID, "uid") || c.History.Selection.Revision != "" {
		return invalid
	}
	targets := append([]memoryhistory.Target(nil), c.History.Selection.Targets...)
	for _, series := range c.History.Series {
		targets = append(targets, series.Target)
	}
	for _, t := range targets {
		if !alias(t.Namespace, "namespace") || !alias(t.Pod, "pod") || !alias(t.PodUID, "uid") || !alias(t.Node, "node") || !alias(t.NodeUID, "uid") {
			return invalid
		}
		if (t.Container != "" && !alias(t.Container, "container")) || (t.ContainerID != "" && !alias(t.ContainerID, "container-id")) {
			return invalid
		}
	}
	for _, m := range c.Changes.Markers {
		if !alias(m.SourceUID, "uid") || (m.PreviousUID != "" && !alias(m.PreviousUID, "uid")) || (m.Container != "" && !alias(m.Container, "container")) {
			return invalid
		}
		objects := append([]changemarkers.Object{m.Subject}, m.Owners...)
		for _, o := range objects {
			if !alias(o.Namespace, "namespace") || !alias(o.Name, strings.ToLower(o.Kind)) || !alias(o.UID, "uid") {
				return invalid
			}
		}
	}
	return nil
}
