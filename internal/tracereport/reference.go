package tracereport

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/traceclient"
)

const ReferenceSchema = 1
const OperatorSupplied = "operator-supplied"

// Reference identifies exact operator-supplied report bytes. Validation proves
// their shape, never their producer, target namespace or measurement authenticity.
// There is no filename, URL, free-form text or trace payload in this projection.
type Reference struct {
	SchemaVersion       int                 `json:"schemaVersion"`
	Provenance          string              `json:"provenance"`
	Digest              string              `json:"digest"`
	Bytes               int                 `json:"bytes"`
	ReportSchemaVersion int                 `json:"reportSchemaVersion"`
	CapturedAt          time.Time           `json:"capturedAt"`
	TraceKind           string              `json:"traceKind"`
	Contract            string              `json:"contract"`
	StreamVersion       int                 `json:"streamVersion"`
	EngineDigest        string              `json:"engineDigest"`
	ProgrammeDigest     string              `json:"programmeDigest"`
	State               traceclient.State   `json:"state"`
	Cleanup             traceclient.Cleanup `json:"cleanup"`
	Failure             string              `json:"failure"`
	CleanupFailure      string              `json:"cleanupFailure"`
	TransportComplete   bool                `json:"transportComplete"`
	Termination         string              `json:"termination"`
	Coverage            string              `json:"coverage"`
	Produced            ReferenceCount      `json:"produced"`
	Sampled             ReferenceCount      `json:"sampled"`
	Lost                ReferenceCount      `json:"lost"`
	Rejected            ReferenceCount      `json:"rejected"`
}

// ReferenceCount distinguishes a reported zero from an unknown count without
// sharing mutable pointers with a caller or retaining its original report.
type ReferenceCount struct {
	Known bool   `json:"known"`
	Value uint64 `json:"value"`
}

func (Reference) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[private trace reference]")
}

// Describe validates the report before deriving a bounded immutable-value
// projection. Hashing includes original whitespace, including any final newline.
func Describe(data []byte) (Reference, error) {
	archive, err := Read(bytes.NewReader(data))
	if err != nil {
		return Reference{}, err
	}
	// Project and hash the same owned bytes that the strict reader validated.
	data = []byte(archive.data)
	var wire archiveWire
	if json.Unmarshal(data, &wire) != nil {
		return Reference{}, ErrInvalid
	}
	sum := sha256.Sum256(data)
	r := Reference{SchemaVersion: ReferenceSchema, Provenance: OperatorSupplied,
		Digest: hex.EncodeToString(sum[:]), Bytes: len(data), ReportSchemaVersion: wire.SchemaVersion,
		CapturedAt: wire.CapturedAt.UTC(), TraceKind: "unreported", Contract: "unreported",
		State: wire.State, Cleanup: wire.Cleanup, Failure: referenceError(wire.Failure),
		CleanupFailure: referenceError(wire.CleanupFailure), TransportComplete: wire.TransportComplete,
		Termination: "unreported", Coverage: "unreported"}
	if wire.ContractVersion != nil {
		r.Contract = "1"
	}
	if wire.Requested != nil {
		r.TraceKind = string(wire.Requested.Kind)
	}
	if wire.Observed != nil {
		r.TraceKind = string(wire.Observed.Kind)
		r.StreamVersion = wire.Observed.StreamVersion
		r.EngineDigest, r.ProgrammeDigest = wire.Observed.EngineDigest, wire.Observed.ProgrammeDigest
	}
	if !bytes.Equal(bytes.TrimSpace(wire.Summary), []byte("null")) {
		var summary summaryWire
		if json.Unmarshal(wire.Summary, &summary) != nil {
			return Reference{}, ErrInvalid
		}
		r.Termination, r.Coverage = string(summary.Termination), "complete"
		if summary.Incomplete {
			r.Coverage = "incomplete"
		}
		r.Produced = referenceCount(summary.EngineCounts.Produced)
		r.Sampled = referenceCount(summary.EngineCounts.Sampled)
		r.Lost = referenceCount(summary.EngineCounts.Lost)
		r.Rejected = referenceCount(summary.EngineCounts.Rejected)
	}
	if err := r.Validate(); err != nil {
		return Reference{}, err
	}
	return r, nil
}

// VerifyReference establishes agreement with a retained file, not provenance.
// A structurally valid imported reference cannot grant access to other evidence.
func VerifyReference(reference Reference, data []byte) error {
	if reference.Validate() != nil {
		return ErrInvalid
	}
	actual, err := Describe(data)
	if err != nil {
		return err
	}
	// Compare times by instant so equivalent JSON timezone offsets remain valid.
	reference.CapturedAt = reference.CapturedAt.UTC()
	if reference != actual {
		return ErrInvalid
	}
	return nil
}

func referenceCount(value *uint64) ReferenceCount {
	if value == nil {
		return ReferenceCount{}
	}
	return ReferenceCount{Known: true, Value: *value}
}

func referenceError(value *string) string {
	if value == nil {
		return "none"
	}
	return *value
}
