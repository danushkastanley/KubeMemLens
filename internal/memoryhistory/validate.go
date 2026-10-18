package memoryhistory

import (
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/validation"
)

func (r Request) Validate() error {
	if len(validation.IsDNS1123Subdomain(r.Name)) != 0 {
		return ErrInvalid
	}
	switch r.Scope {
	case Node:
		if r.Namespace != "" || r.Container != "" || r.WorkloadKind != "" {
			return ErrInvalid
		}
	case Pod, Container, Workload:
		if len(validation.IsDNS1123Label(r.Namespace)) != 0 {
			return ErrInvalid
		}
		if (r.Scope == Container) != (r.Container != "") || (r.Scope == Workload) != (r.WorkloadKind != "") {
			return ErrInvalid
		}
		if r.Container != "" && len(validation.IsDNS1123Label(r.Container)) != 0 {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

func (q Query) Validate(now time.Time) error {
	if (q.Source != Local && q.Source != Prometheus) || (q.Metric != Charge && q.Metric != WorkingSet && q.Metric != RSS) {
		return ErrInvalid
	}
	if q.Start.IsZero() || q.End.Before(q.Start) || q.End.After(now) || q.Start.Nanosecond() != 0 || q.End.Nanosecond() != 0 {
		return ErrInvalid
	}
	if q.Step < MinStep || q.Step > time.Hour || q.Step%time.Second != 0 || q.End.Sub(q.Start) > MaxRange || q.End.Sub(q.Start)/q.Step >= MaxPoints {
		return ErrBounds
	}
	return nil
}

func (s Selection) Validate() error {
	if s.Request.Validate() != nil || !validIdentity(s.UID) || len(s.Revision) > 128 || s.ResolvedAt.IsZero() {
		return ErrInvalid
	}
	if len(s.Targets) == 0 || len(s.Targets) > MaxTargets {
		return ErrBounds
	}
	seen := map[string]bool{}
	for _, t := range s.Targets {
		key := t.Namespace + "/" + t.Pod + "/" + t.Container
		if seen[key] || len(validation.IsDNS1123Subdomain(t.Node)) != 0 || !validIdentity(t.NodeUID) || t.StartedAt.IsZero() || t.StartedAt.After(s.ResolvedAt) {
			return ErrInvalid
		}
		seen[key] = true
		if s.Request.Scope == Node {
			if len(s.Targets) != 1 || t.Node != s.Request.Name || t.NodeUID != s.UID || t.Namespace != "" || t.Pod != "" || t.PodUID != "" || t.Container != "" || t.ContainerID != "" {
				return ErrInvalid
			}
			continue
		}
		if t.Namespace != s.Request.Namespace || len(validation.IsDNS1123Subdomain(t.Pod)) != 0 || !validIdentity(t.PodUID) || len(validation.IsDNS1123Label(t.Container)) != 0 || !validIdentity(t.ContainerID) {
			return ErrInvalid
		}
		if s.Request.Scope != Workload && (t.Pod != s.Request.Name || t.PodUID != s.UID) {
			return ErrInvalid
		}
		if s.Request.Scope == Container && (len(s.Targets) != 1 || t.Container != s.Request.Container) {
			return ErrInvalid
		}
	}
	return nil
}

func validIdentity(s string) bool {
	return len(s) > 0 && len(s) <= 256 && strings.IndexFunc(s, func(r rune) bool { return r < 33 || r > 126 }) < 0
}
