package traceaggregate

import "github.com/danushkastanley/kube-memlens/internal/trace"

// OOMCounts retains no PID, command, target or event timestamp.
type OOMCounts struct {
	Cgroup, Global, Unknown, MissingProcessContext uint64
}

func (a *Accumulator) OOM(event trace.OOMDecision) error {
	command := event.Command.Reveal()
	if a.kind != trace.OOM || event.ValidateContext() != nil {
		return ErrObservation
	}
	if a.observations >= a.limit {
		return ErrLimit
	}
	a.observations++
	switch event.Scope {
	case trace.OOMScopeCgroup:
		a.oom.Cgroup++
	case trace.OOMScopeGlobal:
		a.oom.Global++
	case trace.OOMScopeUnknown:
		a.oom.Unknown++
	}
	if event.VictimPID == nil || command == "" {
		a.oom.MissingProcessContext++
	}
	return nil
}
