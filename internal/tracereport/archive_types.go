package tracereport

import (
	"encoding/json"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceclient"
)

// Archive fields mirror the redacted representation, never the private frame.
// Separate DTOs keep exact JSON vocabulary from leaking into domain structures.
type archiveWire struct {
	SchemaVersion   int                 `json:"schemaVersion"`
	Kind            string              `json:"kind"`
	CapturedAt      time.Time           `json:"capturedAt"`
	ToolVersion     string              `json:"toolVersion"`
	Redacted        bool                `json:"redacted"`
	State           traceclient.State   `json:"state"`
	Cleanup         traceclient.Cleanup `json:"cleanup"`
	Failure         *string             `json:"failure"`
	CleanupFailure  *string             `json:"cleanupFailure"`
	ContractVersion *uint16             `json:"contractVersion,omitempty"`
	Target          struct {
		Namespace string `json:"namespace"`
		Pod       string `json:"pod"`
		Container string `json:"container"`
	} `json:"target"`
	Requested            *intentWire     `json:"requested,omitempty"`
	Observed             *observedWire   `json:"observed,omitempty"`
	Summary              json.RawMessage `json:"summary"`
	Caveats              []string        `json:"caveats"`
	TransportComplete    bool            `json:"transportComplete"`
	ValidatedStreamBytes uint64          `json:"validatedStreamBytes"`
	ValidatedEventFrames uint64          `json:"validatedEventFrames"`
}
type boundsWire struct {
	DurationNanos int64  `json:"durationNanos"`
	Events        uint64 `json:"events"`
	OutputBytes   uint64 `json:"outputBytes"`
	MapBytes      uint64 `json:"mapBytes"`
	PathBytes     uint64 `json:"pathBytes"`
}

func (b boundsWire) domain() trace.Bounds {
	return trace.Bounds{Duration: time.Duration(b.DurationNanos), Events: b.Events, OutputBytes: b.OutputBytes, MapBytes: b.MapBytes, PathBytes: b.PathBytes}
}

type intentWire struct {
	Kind   trace.Kind       `json:"kind"`
	Paths  trace.PathPolicy `json:"paths"`
	Bounds boundsWire       `json:"bounds"`
}
type observedWire struct {
	intentWire
	StreamVersion    int       `json:"streamVersion"`
	EngineDigest     string    `json:"engineDigest"`
	ProgrammeDigest  string    `json:"programmeDigest"`
	SessionStartedAt time.Time `json:"sessionStartedAt"`
	Deadline         time.Time `json:"deadline"`
}
type summaryWire struct {
	SessionEndedAt       time.Time         `json:"sessionEndedAt"`
	ObservationStartedAt *time.Time        `json:"observationStartedAt"`
	ObservationEndedAt   *time.Time        `json:"observationEndedAt"`
	Termination          trace.Termination `json:"termination"`
	Incomplete           bool              `json:"incomplete"`
	EngineCounts         struct {
		Produced *uint64 `json:"produced"`
		Sampled  *uint64 `json:"sampled"`
		Lost     *uint64 `json:"lost"`
		Rejected *uint64 `json:"rejected"`
	} `json:"engineCounts"`
	WrittenEvents             uint64              `json:"writtenEvents"`
	RejectedEvents            uint64              `json:"rejectedEvents"`
	WrittenBytesBeforeSummary uint64              `json:"writtenBytesBeforeSummary"`
	Aggregates                *aggregateWire      `json:"aggregates,omitempty"`
	Correlation               *correlationWire    `json:"correlation,omitempty"`
	OOMCorrelation            *oomCorrelationWire `json:"oomCorrelation,omitempty"`
	KubernetesContext         *kubernetesWire     `json:"kubernetesContext,omitempty"`
}
type totalWire struct {
	Value      *uint64 `json:"value"`
	Unreported bool    `json:"unreported"`
	Overflow   bool    `json:"overflow"`
}
type fileOperationsWire struct {
	Operations     uint64    `json:"operations"`
	RequestedBytes totalWire `json:"requestedBytes"`
	CompletedBytes totalWire `json:"completedBytes"`
}
type cacheOperationsWire struct {
	Operations uint64    `json:"operations"`
	Pages      totalWire `json:"pages"`
}
type aggregateWire struct {
	Kind         trace.Kind           `json:"kind"`
	Observations uint64               `json:"observations"`
	Reads        *fileOperationsWire  `json:"reads,omitempty"`
	Writes       *fileOperationsWire  `json:"writes,omitempty"`
	Additions    *cacheOperationsWire `json:"additions,omitempty"`
	Removals     *cacheOperationsWire `json:"removals,omitempty"`
	Decisions    *struct {
		Cgroup                uint64 `json:"cgroup"`
		Global                uint64 `json:"global"`
		Unknown               uint64 `json:"unknown"`
		MissingProcessContext uint64 `json:"missingProcessContext"`
	} `json:"decisions,omitempty"`
}
type gaugeWire struct {
	Before *uint64 `json:"before"`
	After  *uint64 `json:"after"`
}
type deltaWire struct {
	State string  `json:"state"`
	Delta *uint64 `json:"delta"`
}
type windowWire struct {
	State            string    `json:"state"`
	EvidenceStart    time.Time `json:"evidenceStart,omitempty"`
	BeforeEnd        time.Time `json:"beforeEnd,omitempty"`
	AfterStart       time.Time `json:"afterStart,omitempty"`
	EvidenceEnd      time.Time `json:"evidenceEnd,omitempty"`
	OverlapStart     time.Time `json:"overlapStart,omitempty"`
	OverlapEnd       time.Time `json:"overlapEnd,omitempty"`
	UncertaintyNanos *int64    `json:"uncertaintyNanos,omitempty"`
}
type correlationWire struct {
	windowWire
	FileBytes      gaugeWire `json:"fileBytes,omitempty"`
	DirtyBytes     gaugeWire `json:"dirtyBytes,omitempty"`
	WritebackBytes gaugeWire `json:"writebackBytes,omitempty"`
	Refault        deltaWire `json:"refault,omitempty"`
	Scan           deltaWire `json:"scan,omitempty"`
	Steal          deltaWire `json:"steal,omitempty"`
}
type eventsWire struct {
	Low          deltaWire `json:"low"`
	High         deltaWire `json:"high"`
	Max          deltaWire `json:"max"`
	OOM          deltaWire `json:"oom"`
	OOMKill      deltaWire `json:"oomKill"`
	OOMGroupKill deltaWire `json:"oomGroupKill"`
}
type limitWire struct {
	State string  `json:"state"`
	Bytes *uint64 `json:"bytes"`
}
type oomCorrelationWire struct {
	Window          windowWire `json:"window"`
	Local           eventsWire `json:"local,omitempty"`
	Hierarchical    eventsWire `json:"hierarchical,omitempty"`
	CurrentBytes    gaugeWire  `json:"currentBytes,omitempty"`
	LimitBefore     limitWire  `json:"limitBefore,omitempty"`
	LimitAfter      limitWire  `json:"limitAfter,omitempty"`
	SomeStallMicros deltaWire  `json:"someStallMicros,omitempty"`
	FullStallMicros deltaWire  `json:"fullStallMicros,omitempty"`
}
type kubernetesWire struct {
	State          string    `json:"state"`
	BeforeStart    time.Time `json:"beforeStart,omitempty"`
	BeforeEnd      time.Time `json:"beforeEnd,omitempty"`
	AfterStart     time.Time `json:"afterStart,omitempty"`
	AfterEnd       time.Time `json:"afterEnd,omitempty"`
	Restarts       deltaWire `json:"restarts,omitempty"`
	PressureBefore string    `json:"pressureBefore,omitempty"`
	PressureAfter  string    `json:"pressureAfter,omitempty"`
}
