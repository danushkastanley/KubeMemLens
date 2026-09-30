package tracereport

import (
	"math/bits"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceclient"
	"github.com/danushkastanley/kube-memlens/internal/tracecompat"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

// Validate checks retained reference semantics. Only Describe binds these fields
// to report bytes; neither operation authenticates the report's original source.
func (r Reference) Validate() error {
	if r.SchemaVersion != ReferenceSchema || r.Provenance != OperatorSupplied ||
		len(r.Digest) != 64 || strings.Trim(r.Digest, "0123456789abcdef") != "" ||
		r.Bytes < 1 || r.Bytes > MaxBytes || r.ReportSchemaVersion < MinimumSchema ||
		r.ReportSchemaVersion > CurrentSchema || r.CapturedAt.IsZero() || r.Outcome().Validate() != nil {
		return ErrInvalid
	}
	// Imported offsets may move an otherwise valid JSON year outside the
	// representable range when the retained timestamp is normalised to UTC.
	for _, instant := range []time.Time{r.CapturedAt, r.CapturedAt.UTC()} {
		if _, err := instant.MarshalJSON(); err != nil {
			return ErrInvalid
		}
	}
	for _, failure := range []string{r.Failure, r.CleanupFailure} {
		if failure != "none" && !archiveError(&failure, r.ReportSchemaVersion) {
			return ErrInvalid
		}
	}
	if r.Contract != "unreported" && (r.Contract != "1" || r.ReportSchemaVersion != CurrentSchema) {
		return ErrInvalid
	}
	if r.StreamVersion == 0 {
		if r.EngineDigest != "" || r.ProgrammeDigest != "" || r.TransportComplete || r.Termination != "unreported" {
			return ErrInvalid
		}
	} else if !traceframe.AllowsKind(r.StreamVersion, trace.Kind(r.TraceKind)) ||
		!reportDigest(r.EngineDigest) || !reportDigest(r.ProgrammeDigest) ||
		(r.Contract == "1" && !tracecompat.StreamCompatible(trace.Kind(r.TraceKind), r.StreamVersion)) {
		return ErrInvalid
	}
	for _, count := range []ReferenceCount{r.Produced, r.Sampled, r.Lost, r.Rejected} {
		if !count.Known && count.Value != 0 {
			return ErrInvalid
		}
	}
	if r.StreamVersion != 0 && r.StreamVersion != traceframe.Version {
		if r.Coverage == "complete" {
			return ErrInvalid
		}
		var lower uint64
		for _, count := range []ReferenceCount{r.Sampled, r.Lost, r.Rejected} {
			value, carry := bits.Add64(lower, count.Value, 0)
			if carry != 0 {
				return ErrInvalid
			}
			lower = value
		}
		if r.Produced.Known && lower > r.Produced.Value {
			return ErrInvalid
		}
	}
	if r.Termination == "unreported" && (r.Produced.Known || r.Sampled.Known || r.Lost.Known || r.Rejected.Known) {
		return ErrInvalid
	}
	if r.State == traceclient.StateCompleted && r.Failure != "none" {
		return ErrInvalid
	}
	if r.Coverage == "complete" && (!r.Produced.Known || !r.Sampled.Known || !r.Lost.Known || !r.Rejected.Known ||
		r.Sampled.Value != 0 || r.Lost.Value != 0 || r.Rejected.Value != 0) {
		return ErrInvalid
	}
	return nil
}
