package traceframe

import (
	"encoding/json"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceevidence"
)

// WithOOMKubernetesContext is the control service's explicit encoding boundary.
// Node-provided Kubernetes context is forbidden, including an unavailable claim.
func (f Frame) WithOOMKubernetesContext(context *trace.KubernetesOOMContext, ended time.Time, duration time.Duration) (Frame, error) {
	if f.version != OOMVersion || f.kind != SummaryFrame || context == nil {
		return Frame{}, ErrInvalid
	}
	var value envelope
	if json.Unmarshal([]byte(f.data), &value) != nil || value.Summary == nil || len(value.Summary.KubernetesContext) != 0 || ended.Before(value.Summary.SessionEndedAt) {
		return Frame{}, ErrInvalid
	}
	if value.Summary.Termination == trace.AuthorisationLost {
		return f, nil
	}
	encoded, err := traceevidence.KubernetesOOMEncode(context, duration)
	if err != nil {
		return Frame{}, ErrInvalid
	}
	value.Summary.KubernetesContext = encoded
	value.Summary.SessionEndedAt = ended.UTC()
	if validateSummary(*value.Summary, OOMVersion) != nil {
		return Frame{}, ErrInvalid
	}
	result, err := makeFrame(value)
	if err != nil || len(result.data) > OOMTerminalReserve {
		return Frame{}, ErrInvalid
	}
	return result, nil
}
