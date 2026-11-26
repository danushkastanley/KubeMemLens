package incident

import (
	"regexp"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

var aliasKindPattern = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// Redact prepares a schema 1 or 2 bundle for sharing. It removes Pod UIDs,
// container IDs, cgroup paths, labels and memory record names, and replaces
// every namespace, Pod, Node, container, owner and workload name with a
// capture-local alias, as the newer incident schemas do. The runtime class
// names a cluster object and is removed, as in volume captures. Kinds, phases,
// QoS classes and termination reasons are categories and stay.
//
// The bundle shape does not change, so older readers still read the file.
func Redact(bundle *api.IncidentBundle) {
	a := newCaptureAliases()
	for i := range bundle.Pods {
		redactDeepPod(a, &bundle.Pods[i])
	}
	for i := range bundle.Histories {
		h := &bundle.Histories[i]
		h.PodName = a.scoped("pod", h.Namespace, h.PodName)
		h.Namespace = a.name("namespace", h.Namespace)
		h.NodeName = a.name("node", h.NodeName)
		h.PodUID = ""
	}
	for i := range bundle.Nodes {
		bundle.Nodes[i].NodeName = a.name("node", bundle.Nodes[i].NodeName)
	}
}

func redactDeepPod(a *captureAliases, p *api.PodSnapshot) {
	namespace := p.Namespace
	p.PodName = a.scoped("pod", namespace, p.PodName)
	p.Namespace = a.name("namespace", namespace)
	p.NodeName = a.name("node", p.NodeName)
	p.PodUID, p.Memory.Name = "", ""
	c := &p.Context
	c.OwnerName = a.reference(c.OwnerKind, namespace, c.OwnerName)
	c.WorkloadName = a.reference(c.WorkloadKind, namespace, c.WorkloadName)
	c.RuntimeClassName, c.Labels = "", nil
	for i := range p.Containers {
		redactDeepContainer(a, &p.Containers[i])
	}
}

// A container keeps its own namespace and Pod fields; they are aliased from
// their own values so that a container never borrows another Pod's alias.
func redactDeepContainer(a *captureAliases, s *api.ContainerSnapshot) {
	namespace, pod := s.Namespace, s.PodName
	s.ContainerName = a.scoped("container", namespace+"/"+pod, s.ContainerName)
	s.PodName = a.scoped("pod", namespace, pod)
	s.Namespace = a.name("namespace", namespace)
	s.NodeName = a.name("node", s.NodeName)
	s.PodUID, s.ContainerID, s.CgroupPath, s.Memory.Name = "", "", "", ""
	c := &s.Context
	c.OwnerName = a.reference(c.OwnerKind, namespace, c.OwnerName)
	c.WorkloadName = a.reference(c.WorkloadKind, namespace, c.WorkloadName)
	c.RuntimeClassName, c.Labels = "", nil
}

// scoped aliases a name that is unique only within its scope, such as a Pod
// within its namespace. An absent name stays absent.
func (a *captureAliases) scoped(kind, scope, name string) string {
	if name == "" {
		return ""
	}
	return a.name(kind, scope+"/"+name)
}

// reference aliases an owner or workload by its kind, as history captures do,
// so a StatefulSet that is both owner and workload keeps one alias. A Node owns
// its static Pods and is not namespaced, so it keeps the alias of its name. A
// kind that cannot form an alias falls back to "object".
func (a *captureAliases) reference(kind, namespace, name string) string {
	prefix := strings.ToLower(kind)
	if prefix == "node" {
		return a.name("node", name)
	}
	if !aliasKindPattern.MatchString(prefix) {
		prefix = "object"
	}
	return a.scoped(prefix, namespace, name)
}
