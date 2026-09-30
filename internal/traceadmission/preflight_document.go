package traceadmission

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

// PreflightDocument contains no workload identifiers or private runtime handles.
// It is an authorised inspection response, not an admission or trace result.
type PreflightDocument struct {
	SchemaVersion        int              `json:"schemaVersion"`
	Kind                 string           `json:"kind"`
	RequestSchemaVersion int              `json:"requestSchemaVersion"`
	CheckedAt            time.Time        `json:"checkedAt"`
	TraceKind            trace.Kind       `json:"traceKind"`
	Paths                trace.PathPolicy `json:"paths"`
	Bounds               PreflightBounds  `json:"bounds"`
	Node                 NodePreflight    `json:"node"`
	ResourceQualified    bool             `json:"resourceQualified"`
}

type PreflightBounds struct {
	DurationNanos int64  `json:"durationNanos"`
	Events        uint64 `json:"events"`
	OutputBytes   uint64 `json:"outputBytes"`
	MapBytes      uint64 `json:"mapBytes"`
	PathBytes     uint64 `json:"pathBytes"`
}

func NewPreflightDocument(r Request, node NodePreflight, checkedAt time.Time) (PreflightDocument, error) {
	if r.selection == nil || node.Validate(r.Kind()) != nil || checkedAt.IsZero() {
		return PreflightDocument{}, ErrUnavailable
	}
	b := r.Bounds()
	return PreflightDocument{SchemaVersion: 1, Kind: "TracePreflight", RequestSchemaVersion: 2,
		CheckedAt: checkedAt.UTC(), TraceKind: r.Kind(), Paths: r.Paths(),
		Bounds: PreflightBounds{int64(b.Duration), b.Events, b.OutputBytes, b.MapBytes, b.PathBytes},
		Node:   node, ResourceQualified: false}, nil
}
