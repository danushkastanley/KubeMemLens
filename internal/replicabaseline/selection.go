package replicabaseline

import (
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
)

func (s Selection) Validate() error {
	if s.Request.Validate() != nil || (s.Request.Scope != memoryhistory.Pod && s.Request.Scope != memoryhistory.Workload) || s.Requested.Validate() != nil || s.Workload.Validate() != nil || s.Workload.Kind == "Pod" || s.Requested.Namespace != s.Request.Namespace || s.Requested.Name != s.Request.Name || s.Workload.Namespace != s.Request.Namespace || s.Generation < 0 || s.ResolvedAt.IsZero() {
		return ErrInvalid
	}
	if s.Request.Scope == memoryhistory.Pod && s.Requested.Kind != "Pod" || s.Request.Scope == memoryhistory.Workload && s.Requested.Kind != s.Request.WorkloadKind {
		return ErrInvalid
	}
	if len(s.Members) > MaxPeers {
		return ErrBounds
	}
	seen, names := map[string]bool{}, map[string]bool{}
	selected := s.Request.Scope == memoryhistory.Workload
	for _, m := range s.Members {
		if m.Object.Validate() != nil || m.Object.Kind != "Pod" || m.Object.Namespace != s.Workload.Namespace || seen[m.Object.UID] || names[m.Object.Name] || len(m.Containers) > MaxContainers || len(m.Owners) == 0 || m.Owners[len(m.Owners)-1] != s.Workload {
			return ErrInvalid
		}
		seen[m.Object.UID] = true
		names[m.Object.Name] = true
		for _, owner := range m.Owners {
			if owner.Validate() != nil || owner.Namespace != s.Workload.Namespace {
				return ErrInvalid
			}
		}
		if s.Request.Scope == memoryhistory.Pod && m.Object == s.Requested {
			selected = true
		}
	}
	if !selected {
		return ErrScope
	}
	return nil
}
