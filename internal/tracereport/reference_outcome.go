package tracereport

import (
	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceclient"
)

// ReferenceOutcome is the non-identifying outcome vocabulary shared by private
// references and sanitised timelines. Counters and artefact digests stay private.
type ReferenceOutcome struct {
	TraceKind         string              `json:"traceKind"`
	State             traceclient.State   `json:"state"`
	Cleanup           traceclient.Cleanup `json:"cleanup"`
	TransportComplete bool                `json:"transportComplete"`
	Termination       string              `json:"termination"`
	Coverage          string              `json:"coverage"`
	Loss              string              `json:"loss"`
}

func (r Reference) Outcome() ReferenceOutcome {
	loss := "unreported"
	if r.Lost.Known {
		loss = "none-reported"
		if r.Lost.Value != 0 {
			loss = "reported"
		}
	}
	return ReferenceOutcome{r.TraceKind, r.State, r.Cleanup, r.TransportComplete, r.Termination, r.Coverage, loss}
}

func (o ReferenceOutcome) Validate() error {
	if !o.State.Terminal() || (o.Cleanup != traceclient.CleanupConfirmed && o.Cleanup != traceclient.CleanupUnconfirmed && o.Cleanup != traceclient.CleanupNotRequested) {
		return ErrInvalid
	}
	switch o.TraceKind {
	case "unreported", string(trace.Files), string(trace.Cache), string(trace.OOM):
	default:
		return ErrInvalid
	}
	if o.Loss != "unreported" && o.Loss != "none-reported" && o.Loss != "reported" {
		return ErrInvalid
	}
	if o.Termination == "unreported" {
		if o.Coverage != "unreported" || o.Loss != "unreported" || o.TransportComplete || o.State == traceclient.StateCompleted || o.State == traceclient.StateTruncated {
			return ErrInvalid
		}
		return nil
	}
	if o.TraceKind == "unreported" || (o.Coverage != "complete" && o.Coverage != "incomplete") {
		return ErrInvalid
	}
	switch trace.Termination(o.Termination) {
	case trace.Expired, trace.Cancelled, trace.TargetChanged, trace.OutputLimit, trace.EventLimit, trace.EngineFailed, trace.AuthorisationLost:
	default:
		return ErrInvalid
	}
	if o.State == traceclient.StateCompleted && (o.Termination != string(trace.Expired) || !o.TransportComplete) {
		return ErrInvalid
	}
	if o.State == traceclient.StateTruncated && ((o.Termination != string(trace.EventLimit) && o.Termination != string(trace.OutputLimit)) || !o.TransportComplete) {
		return ErrInvalid
	}
	if o.Coverage == "complete" && (o.Termination != string(trace.Expired) || o.Loss != "none-reported") {
		return ErrInvalid
	}
	return nil
}
