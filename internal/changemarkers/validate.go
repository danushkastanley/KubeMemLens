package changemarkers

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"k8s.io/apimachinery/pkg/util/validation"
)

func (o Object) Validate() error {
	if len(validation.IsDNS1123Subdomain(o.Name)) != 0 || len(validation.IsDNS1123Label(o.Namespace)) != 0 || !identity(o.UID) {
		return ErrInvalid
	}
	version := ""
	switch o.Kind {
	case "Pod", "ReplicationController":
		version = "v1"
	case "Deployment", "ReplicaSet", "StatefulSet", "DaemonSet":
		version = "apps/v1"
	case "Job", "CronJob":
		version = "batch/v1"
	}
	if version == "" || o.APIVersion != version {
		return ErrInvalid
	}
	return nil
}

func (m Marker) Validate() error {
	if m.Subject.Validate() != nil || m.At.IsZero() || m.Until.Before(m.At) || !identity(m.SourceUID) || m.Count == 0 || m.Count > 1<<31-1 || len(m.Owners) > MaxOwners {
		return ErrInvalid
	}
	if m.Container != "" && (m.Subject.Kind != "Pod" || len(validation.IsDNS1123Label(m.Container)) != 0) {
		return ErrInvalid
	}
	seen := map[string]bool{m.Subject.UID: true}
	for _, owner := range m.Owners {
		if owner.Validate() != nil || owner.Kind == "Pod" || owner.Namespace != m.Subject.Namespace || seen[owner.UID] {
			return ErrInvalid
		}
		seen[owner.UID] = true
	}
	ownerRevision := m.Kind == Revision && m.Subject.Kind == "Pod" && slices.ContainsFunc(m.Owners, func(o Object) bool {
		return o.UID == m.SourceUID && (o.Kind == "ReplicaSet" || o.Kind == "Deployment")
	})
	if m.Clock != KubernetesEvent && ((!ownerRevision && m.SourceUID != m.Subject.UID) || m.Count != 1) {
		return ErrInvalid
	}
	if m.Clock != Observation && m.Clock != KubernetesEvent && !m.At.Equal(m.Until) {
		return ErrInvalid
	}
	if m.Clock == Observation && !m.Uncertain {
		return ErrInvalid
	}
	if m.Revision < 0 || (m.Kind == Revision) != (m.Revision > 0) || (m.Kind == Replacement) != (m.PreviousUID != "") {
		return ErrInvalid
	}
	if m.Kind != Resize && m.ResizeState != "" {
		return ErrInvalid
	}
	switch m.Kind {
	case Created:
		if m.Clock != APICreation {
			return ErrInvalid
		}
	case Restarted:
		if m.Clock != ContainerState || m.Container == "" {
			return ErrInvalid
		}
	case Revision:
		if m.Clock != Observation || (!ownerRevision && m.Subject.Kind != "ReplicaSet" && m.Subject.Kind != "Deployment") {
			return ErrInvalid
		}
	case Replacement:
		if m.Clock != Observation || m.Subject.Kind != "Pod" || !identity(m.PreviousUID) || m.PreviousUID == m.Subject.UID || m.Container != "" {
			return ErrInvalid
		}
	case Resize:
		if m.Subject.Kind != "Pod" || (m.Clock != PodCondition && m.Clock != KubernetesEvent && m.Clock != Observation) {
			return ErrInvalid
		}
		switch m.ResizeState {
		case "started", "completed", "deferred", "infeasible", "error":
			if m.Clock != KubernetesEvent {
				return ErrInvalid
			}
		case "pending", "in-progress":
			if m.Clock != PodCondition {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
	case Rollout, Scheduled, Started, Stopped, BackOff, Evicted:
		if m.Clock != KubernetesEvent {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

func (r Report) Validate() error {
	if r.SchemaVersion != 1 || r.Request.Validate() != nil || r.Request.Scope == memoryhistory.Node || !identity(r.UID) || r.Start.IsZero() || r.End.Before(r.Start) || r.End.Sub(r.Start) > memoryhistory.MaxRange || r.ObservedAt.Before(r.End) {
		return ErrInvalid
	}
	if r.Request.Scope == memoryhistory.Workload {
		switch r.Request.WorkloadKind {
		case "Deployment", "ReplicaSet", "StatefulSet", "DaemonSet", "ReplicationController", "Job", "CronJob":
		default:
			return ErrInvalid
		}
	}
	switch r.Events {
	case Partial, Missing, Denied, Unavailable, Disabled:
	default:
		return ErrInvalid
	}
	if len(r.Markers) > MaxMarkers {
		return ErrBounds
	}
	seen := map[string]bool{}
	for i, marker := range r.Markers {
		key := markerKey(marker)
		if marker.Validate() != nil || marker.Subject.Namespace != r.Request.Namespace || !r.includes(marker) || marker.Until.After(r.ObservedAt) || seen[key] {
			return ErrInvalid
		}
		if marker.Clock == KubernetesEvent && r.Events != Partial {
			return ErrInvalid
		}
		// Revision is current observed context, not a retrospectively timed change.
		if marker.Kind != Revision && (marker.Until.Before(r.Start) || marker.At.After(r.End)) {
			return ErrInvalid
		}
		if marker.Kind == Revision && (!marker.At.Equal(r.ObservedAt) || !marker.Until.Equal(r.ObservedAt)) {
			return ErrInvalid
		}
		if i > 0 && compareMarker(r.Markers[i-1], marker) >= 0 {
			return ErrInvalid
		}
		seen[key] = true
	}
	data, err := json.Marshal(r)
	if err != nil || len(data) > MaxReportBytes {
		return ErrBounds
	}
	return nil
}

func (r Report) includes(m Marker) bool {
	if r.Request.Scope != memoryhistory.Workload {
		return m.Subject.Kind == "Pod" && m.Subject.Name == r.Request.Name && m.Subject.UID == r.UID &&
			(r.Request.Scope != memoryhistory.Container || m.Container == "" || m.Container == r.Request.Container)
	}
	chain := append([]Object{m.Subject}, m.Owners...)
	return slices.ContainsFunc(chain, func(o Object) bool {
		return o.Kind == r.Request.WorkloadKind && o.Name == r.Request.Name && o.UID == r.UID
	})
}

func (c Context) Validate() error {
	if c.SchemaVersion != 1 || c.History.Validate() != nil {
		return ErrInvalid
	}
	if err := c.Changes.Validate(); err != nil {
		return err
	}
	if c.Changes.Request != c.History.Selection.Request || c.Changes.UID != c.History.Selection.UID || !c.Changes.Start.Equal(c.History.Query.Start) || !c.Changes.End.Equal(c.History.Query.End) || c.Changes.ObservedAt.Before(c.History.Selection.ResolvedAt) {
		return ErrInvalid
	}
	data, err := json.Marshal(c)
	if err != nil || len(data) > memoryhistory.MaxResponseBytes {
		return ErrBounds
	}
	return nil
}

func identity(value string) bool {
	return len(value) > 0 && len(value) <= 256 && strings.IndexFunc(value, func(r rune) bool { return r < 33 || r > 126 }) < 0
}
