package nodeanalysis

import (
	"math"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

// These are product triage thresholds, not kernel or Kubernetes guarantees.
const (
	SomeWarningPercent           = 10.0
	FullWarningPercent           = 1.0
	FullCriticalPercent          = 10.0
	SustainedFullCriticalPercent = 1.0
)

func pressure(input Input, result *Analysis) {
	current := input.Current
	context := current.Context
	if context != nil && fresh(context.CapturedAt, input.Now, nodecontext.StaleAfter) {
		switch context.MemoryPressure {
		case "True":
			result.Signals = append(result.Signals, Signal{Code: "kubernetes-memory-pressure", Source: capability.KubernetesStatus, CapturedAt: context.CapturedAt, Severity: Critical})
			promote(result, Critical)
		case "False":
			if input.SourceAvailability == capability.Available {
				result.Signals = append(result.Signals, Signal{Code: "kubernetes-no-memory-pressure", Source: capability.KubernetesStatus, CapturedAt: context.CapturedAt, Severity: Normal})
				promote(result, Normal)
			}
		}
	}
	if current.Stats == nil || current.Stats.Memory == nil {
		return
	}
	memory := current.Stats.Memory
	if !fresh(memory.CapturedAt, input.Now, nodecontext.StaleAfter) || memory.PSI == nil {
		return
	}
	psi := memory.PSI
	if !validPercent(psi.Some.Avg10) || !validPercent(psi.Full.Avg10) || !validPercent(psi.Full.Avg60) {
		return
	}
	switch {
	case psi.Full.Avg10 >= FullCriticalPercent:
		pressureSignal(result, "node-full-stall-10s", memory.CapturedAt, psi.Full.Avg10, Critical)
	case psi.Full.Avg60 >= SustainedFullCriticalPercent:
		pressureSignal(result, "node-sustained-full-stall-60s", memory.CapturedAt, psi.Full.Avg60, Critical)
	case psi.Full.Avg10 >= FullWarningPercent:
		pressureSignal(result, "node-full-stall-10s", memory.CapturedAt, psi.Full.Avg10, Warning)
	case psi.Some.Avg10 >= SomeWarningPercent:
		pressureSignal(result, "node-some-stall-10s", memory.CapturedAt, psi.Some.Avg10, Warning)
	case input.SourceAvailability == capability.Available:
		pressureSignal(result, "node-low-reported-stall-10s", memory.CapturedAt, psi.Full.Avg10, Normal)
	}
	if result.Severity != Unknown && input.SourceAvailability == capability.Available && current.Evidence.Completeness == capability.Complete {
		result.Confidence = High
	}
}

func pressureSignal(result *Analysis, code string, at time.Time, value float64, severity Severity) {
	result.Signals = append(result.Signals, Signal{Code: code, Source: nodecontext.Source, CapturedAt: at, Severity: severity, Value: &value, Unit: "percent"})
	promote(result, severity)
}

func promote(result *Analysis, severity Severity) {
	priority := map[Severity]int{Unknown: 0, Normal: 1, Warning: 2, Critical: 3}
	if priority[severity] > priority[result.Severity] {
		result.Severity = severity
	}
}

func validPercent(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 100
}
