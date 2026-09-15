package traceframe

import (
	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceaggregate"
)

type wireOOMAggregates struct {
	Observations          uint64 `json:"observations"`
	Cgroup                uint64 `json:"cgroup"`
	Global                uint64 `json:"global"`
	Unknown               uint64 `json:"unknown"`
	MissingProcessContext uint64 `json:"missingProcessContext"`
}

func validateOOMAggregates(s wireSummary) error {
	o := s.OOMAggregates
	if o == nil {
		return nil
	}
	if s.FileAggregates != nil || s.CacheAggregates != nil || o.Cgroup > o.Observations || o.Global > o.Observations-o.Cgroup || o.Unknown != o.Observations-o.Cgroup-o.Global || o.MissingProcessContext > o.Observations || s.WrittenEvents != o.Observations {
		return ErrInvalid
	}
	return nil
}

func oomDomain(o *wireOOMAggregates) *traceaggregate.Summary {
	return &traceaggregate.Summary{Kind: trace.OOM, Observations: o.Observations, OOM: traceaggregate.OOMCounts{Cgroup: o.Cgroup, Global: o.Global, Unknown: o.Unknown, MissingProcessContext: o.MissingProcessContext}}
}

func validateOOMVersionEvent(event wireEvent) error {
	if event.OOM == nil {
		return ErrInvalid
	}
	text, err := DecodeText(event.OOM.Command, 16)
	if err != nil {
		return ErrInvalid
	}
	command, err := trace.NewSensitiveText(text, 16)
	if err != nil || (trace.OOMDecision{Scope: event.OOM.Scope, VictimPID: event.OOM.VictimPID, Command: command}).ValidateContext() != nil {
		return ErrInvalid
	}
	return nil
}
