package traceframe

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

type envelope struct {
	Version  int           `json:"version"`
	Type     Type          `json:"type"`
	Metadata *wireMetadata `json:"metadata,omitempty"`
	Event    *wireEvent    `json:"event,omitempty"`
	Summary  *wireSummary  `json:"summary,omitempty"`
}
type wireMetadata struct {
	SessionID        string           `json:"sessionID"`
	EngineDigest     string           `json:"engineDigest"`
	ProgrammeDigest  string           `json:"programmeDigest"`
	Kind             trace.Kind       `json:"kind"`
	Target           wireTarget       `json:"target"`
	Paths            trace.PathPolicy `json:"paths"`
	Bounds           wireBounds       `json:"bounds"`
	SessionStartedAt time.Time        `json:"sessionStartedAt"`
	Deadline         time.Time        `json:"deadline"`
}
type wireTarget struct {
	Namespace          string    `json:"namespace"`
	Pod                string    `json:"pod"`
	PodUID             string    `json:"podUID"`
	Container          string    `json:"container"`
	ContainerStartedAt time.Time `json:"containerStartedAt"`
	// This digest commits to the full internal lifetime, Node UID and cgroup ID;
	// the latter two runtime identifiers are never included in public metadata.
	BindingDigest string `json:"bindingDigest"`
}
type wireBounds struct {
	DurationNanos int64  `json:"durationNanos"`
	Events        uint64 `json:"events"`
	OutputBytes   uint64 `json:"outputBytes"`
	MapBytes      uint64 `json:"mapBytes"`
	PathBytes     uint64 `json:"pathBytes"`
}
type wireEvent struct {
	ObservedAt time.Time  `json:"observedAt"`
	File       *wireFile  `json:"file,omitempty"`
	Cache      *wireCache `json:"cache,omitempty"`
	OOM        *wireOOM   `json:"oom,omitempty"`
}
type wireFile struct {
	Operation      trace.FileOperation `json:"operation"`
	RequestedBytes *uint64             `json:"requestedBytes"`
	CompletedBytes *uint64             `json:"completedBytes"`
	Path           *string             `json:"path,omitempty"`
}
type wireCache struct {
	Operation trace.CacheOperation `json:"operation"`
	Pages     uint64               `json:"pages"`
}
type wireOOM struct {
	Scope     trace.OOMScope `json:"scope"`
	VictimPID *uint32        `json:"victimPID"`
	Command   string         `json:"command"`
}
type wireCounts struct {
	Produced *uint64 `json:"produced"`
	Sampled  *uint64 `json:"sampled"`
	Lost     *uint64 `json:"lost"`
	Rejected *uint64 `json:"rejected"`
}
type wireSummary struct {
	SessionEndedAt            time.Time            `json:"sessionEndedAt"`
	ObservationStartedAt      *time.Time           `json:"observationStartedAt"`
	ObservationEndedAt        *time.Time           `json:"observationEndedAt"`
	Termination               trace.Termination    `json:"termination"`
	EngineCounts              wireCounts           `json:"engineCounts"`
	WrittenEvents             uint64               `json:"writtenEvents"`
	RejectedEvents            uint64               `json:"rejectedEvents"`
	WrittenBytesBeforeSummary uint64               `json:"writtenBytesBeforeSummary"`
	Incomplete                bool                 `json:"incomplete"`
	FileAggregates            *wireFileAggregates  `json:"fileAggregates,omitempty"`
	CacheAggregates           *wireCacheAggregates `json:"cacheAggregates,omitempty"`
	Correlation               json.RawMessage      `json:"correlation,omitempty"`
}

func makeFrame(e envelope) (Frame, error) {
	if e.Version == 0 {
		e.Version = Version
	}
	if !SupportedVersion(e.Version) {
		return Frame{}, ErrInvalid
	}
	data, err := json.Marshal(e)
	if err != nil || len(data)+1 > MaxBytes {
		return Frame{}, ErrInvalid
	}
	return Frame{kind: e.Type, data: string(append(data, '\n')), version: e.Version}, nil
}
func validDigest(value string) bool {
	return len(value) == 71 && strings.HasPrefix(value, "sha256:") && strings.Trim(value[7:], "0123456789abcdef") == ""
}
func validID(value string) bool {
	return len(value) == 32 && strings.Trim(value, "0123456789abcdef") == ""
}
func validTermination(value trace.Termination) bool {
	switch value {
	case trace.Expired, trace.Cancelled, trace.TargetChanged, trace.OutputLimit, trace.EventLimit, trace.EngineFailed, trace.AuthorisationLost:
		return true
	default:
		return false
	}
}
