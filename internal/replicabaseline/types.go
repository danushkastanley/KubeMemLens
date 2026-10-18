// Package replicabaseline compares an explicitly authorised peer set without
// fetching data, changing diagnosis severity or inferring missing measurements.
package replicabaseline

import (
	"errors"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
)

const (
	MaxPeers          = 16
	MaxContainers     = 8
	MaxHistoryPoints  = 181
	MinReferences     = 4
	MinHistoryPoints  = 12
	MaxResponseBytes  = 256 << 10
	FreshFor          = 30 * time.Second
	MaximumSampleSkew = 5 * time.Second
	StabilityWindow   = 5 * time.Minute
	HistoryLookback   = 6 * time.Minute
	MaximumHistoryGap = 30 * time.Second
)

var ErrInvalid = errors.New("invalid replica evidence")
var ErrScope = errors.New("replica evidence crosses its selected workload")
var ErrBounds = errors.New("replica evidence exceeds its budget")

type DataState string

const (
	Available   DataState = "available"
	Unreported  DataState = "unreported"
	Partial     DataState = "partial"
	Stale       DataState = "stale"
	Unavailable DataState = "unavailable"
)

type Lifecycle string

const (
	Ready       Lifecycle = "ready"
	Unready     Lifecycle = "unready"
	Terminating Lifecycle = "terminating"
	Inactive    Lifecycle = "inactive"
)

type ChangeState string

const (
	CurrentStable ChangeState = "current-stable"
	RecentChange  ChangeState = "recent-change"
	ChangeUnknown ChangeState = "unreported"
)

type Value struct {
	State  DataState `json:"state"`
	Number float64   `json:"number"`
}

type Point struct {
	At    time.Time
	Bytes uint64
}

// Peer contains only the selected object's structured metadata and measurements.
// The caller verifies ownership and permissions; WorkloadUID prevents an accidental
// join between independently acquired workloads with similar names.
type Peer struct {
	Object                    changemarkers.Object
	WorkloadUID               string
	Revision, Shape           string
	Lifecycle                 Lifecycle
	SampleState, HistoryState DataState
	CapturedAt, StableSince   time.Time
	DeltaStartedAt            time.Time
	Changes                   ChangeState
	Values                    map[Metric]Value
	History                   []Point
}

type Input struct {
	Workload   changemarkers.Object
	ObservedAt time.Time
	Peers      []Peer
}

type Exclusion struct {
	Peer   changemarkers.Object `json:"peer"`
	Reason string               `json:"reason"`
}

type Distribution struct {
	Minimum float64 `json:"minimum"`
	Median  float64 `json:"median"`
	Maximum float64 `json:"maximum"`
	MAD     float64 `json:"mad"`
}

type Comparison struct {
	Metric        Metric           `json:"metric"`
	Unit          string           `json:"unit"`
	State         string           `json:"state"`
	Candidate     *float64         `json:"candidate,omitempty"`
	References    []string         `json:"referenceUIDs"`
	Omitted       []MetricOmission `json:"omittedReferences,omitempty"`
	Distribution  *Distribution    `json:"distribution,omitempty"`
	Difference    *float64         `json:"difference,omitempty"`
	ModifiedScore *float64         `json:"modifiedScore,omitempty"`
	Method        string           `json:"method,omitempty"`
	Outlier       string           `json:"outlier"`
	Confidence    string           `json:"confidence"`
}

type MetricOmission struct {
	PeerUID string `json:"peerUID"`
	Reason  string `json:"reason"`
}

type PeerReport struct {
	Peer           changemarkers.Object `json:"peer"`
	CapturedAt     time.Time            `json:"capturedAt,omitzero"`
	StableSince    time.Time            `json:"stableSince,omitzero"`
	DeltaStartedAt time.Time            `json:"deltaStartedAt,omitzero"`
	History        *HistoryWindow       `json:"history,omitempty"`
	Revision       string               `json:"revision,omitempty"`
	Shape          string               `json:"shape,omitempty"`
	Exclusion      string               `json:"exclusion,omitempty"`
	Excluded       []Exclusion          `json:"excludedReferences"`
	Comparisons    []Comparison         `json:"comparisons"`
}

type HistoryWindow struct {
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt"`
	Samples   int       `json:"samples"`
}

type Report struct {
	SchemaVersion int                  `json:"schemaVersion"`
	PolicyVersion int                  `json:"policyVersion"`
	Workload      changemarkers.Object `json:"workload"`
	ObservedAt    time.Time            `json:"observedAt"`
	Peers         []PeerReport         `json:"peers"`
	Caveats       []string             `json:"caveats"`
}
