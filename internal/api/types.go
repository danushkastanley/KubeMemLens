package api

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/model"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const CurrentSnapshotSchemaVersion = 1
const CurrentIncidentSchemaVersion = 1
const CurrentExplanationSchemaVersion = 1
const MemoryAPIGroup = "memory.kubememlens.io"
const MemoryAPIVersion = "v1alpha1"

type AgentSnapshot struct {
	SchemaVersion int                 `json:"schemaVersion"`
	NodeName      string              `json:"nodeName"`
	CapturedAt    time.Time           `json:"capturedAt"`
	Environment   NodeEnvironment     `json:"environment"`
	Containers    []ContainerSnapshot `json:"containers"`
}

type NodeEnvironment struct {
	CgroupVersion            string    `json:"cgroupVersion"`
	CgroupDriver             string    `json:"cgroupDriver"`
	ContainerRuntimes        []string  `json:"containerRuntimes"`
	CgroupReadErrors         int       `json:"cgroupReadErrors"`
	NodeContextAvailable     bool      `json:"nodeContextAvailable"`
	MemoryPressureStatus     string    `json:"memoryPressureStatus"`
	MemoryPressureSince      time.Time `json:"memoryPressureSince"`
	MemoryAllocatableBytes   uint64    `json:"memoryAllocatableBytes"`
	MemoryAllocatableKnown   bool      `json:"memoryAllocatableKnown"`
	WorkloadContextAvailable bool      `json:"workloadContextAvailable"`
	WorkloadContextErrors    int       `json:"workloadContextErrors"`
}

type ContainerSnapshot struct {
	Namespace        string                `json:"namespace"`
	PodName          string                `json:"podName"`
	PodUID           string                `json:"podUID"`
	ContainerName    string                `json:"containerName"`
	ContainerID      string                `json:"containerID"`
	NodeName         string                `json:"nodeName"`
	CgroupPath       string                `json:"cgroupPath"`
	CapturedAt       time.Time             `json:"capturedAt"`
	DeltaStartedAt   time.Time             `json:"deltaStartedAt,omitempty"`
	DeltaWindowKnown bool                  `json:"deltaWindowKnown"`
	Context          ContainerContext      `json:"context"`
	Memory           model.MemoryBreakdown `json:"memory"`
}

// ContainerPage is the bounded read contract used by current clients. Continue
// is opaque and should be supplied unchanged to the next request.
type ContainerPage struct {
	Items    []ContainerSnapshot `json:"items"`
	Continue string              `json:"continue,omitempty"`
}

type ContainerContext struct {
	MemoryRequestBytes         uint64            `json:"memoryRequestBytes"`
	MemoryRequestKnown         bool              `json:"memoryRequestKnown"`
	MemoryLimitBytes           uint64            `json:"memoryLimitBytes"`
	MemoryLimitKnown           bool              `json:"memoryLimitKnown"`
	QoSClass                   string            `json:"qosClass"`
	RestartCount               int32             `json:"restartCount"`
	LastTerminationKnown       bool              `json:"lastTerminationKnown"`
	LastTerminationReason      string            `json:"lastTerminationReason"`
	LastTerminationExitCode    int32             `json:"lastTerminationExitCode"`
	LastTerminationFinishedAt  time.Time         `json:"lastTerminationFinishedAt"`
	PodPhase                   string            `json:"podPhase"`
	PodCreatedAt               time.Time         `json:"podCreatedAt"`
	OwnerKind                  string            `json:"ownerKind"`
	OwnerName                  string            `json:"ownerName"`
	WorkloadKind               string            `json:"workloadKind"`
	WorkloadName               string            `json:"workloadName"`
	NodeMemoryPressure         string            `json:"nodeMemoryPressure"`
	NodeMemoryAllocatable      uint64            `json:"nodeMemoryAllocatable"`
	NodeMemoryAllocatableKnown bool              `json:"nodeMemoryAllocatableKnown"`
	RuntimeClassName           string            `json:"runtimeClassName"`
	MemoryEmptyDirCount        int               `json:"memoryEmptyDirCount"`
	MemoryEmptyDirLimited      int               `json:"memoryEmptyDirLimited"`
	MemoryEmptyDirLimitBytes   uint64            `json:"memoryEmptyDirLimitBytes"`
	Labels                     map[string]string `json:"labels,omitempty"`
}

type PodSnapshot struct {
	Namespace  string                `json:"namespace"`
	PodName    string                `json:"podName"`
	PodUID     string                `json:"podUID"`
	NodeName   string                `json:"nodeName"`
	CapturedAt time.Time             `json:"capturedAt"`
	Containers []ContainerSnapshot   `json:"containers"`
	Context    PodContext            `json:"context"`
	Memory     model.MemoryBreakdown `json:"memory"`
}

type PodContext struct {
	MemoryRequestBytes         uint64            `json:"memoryRequestBytes"`
	MemoryRequestContainers    int               `json:"memoryRequestContainers"`
	MemoryLimitBytes           uint64            `json:"memoryLimitBytes"`
	MemoryLimitContainers      int               `json:"memoryLimitContainers"`
	QoSClass                   string            `json:"qosClass"`
	RestartCount               int32             `json:"restartCount"`
	LastTerminationKnown       bool              `json:"lastTerminationKnown"`
	LastTerminationReason      string            `json:"lastTerminationReason"`
	LastTerminationExitCode    int32             `json:"lastTerminationExitCode"`
	LastTerminationFinishedAt  time.Time         `json:"lastTerminationFinishedAt"`
	Phase                      string            `json:"phase"`
	CreatedAt                  time.Time         `json:"createdAt"`
	OwnerKind                  string            `json:"ownerKind"`
	OwnerName                  string            `json:"ownerName"`
	WorkloadKind               string            `json:"workloadKind"`
	WorkloadName               string            `json:"workloadName"`
	NodeMemoryPressure         string            `json:"nodeMemoryPressure"`
	NodeMemoryAllocatable      uint64            `json:"nodeMemoryAllocatable"`
	NodeMemoryAllocatableKnown bool              `json:"nodeMemoryAllocatableKnown"`
	RuntimeClassName           string            `json:"runtimeClassName"`
	MemoryEmptyDirCount        int               `json:"memoryEmptyDirCount"`
	MemoryEmptyDirLimited      int               `json:"memoryEmptyDirLimited"`
	MemoryEmptyDirLimitBytes   uint64            `json:"memoryEmptyDirLimitBytes"`
	Labels                     map[string]string `json:"labels,omitempty"`
}

type NodeContext struct {
	Available              bool      `json:"available"`
	NodeUID                string    `json:"-"`
	MemoryPressureStatus   string    `json:"memoryPressureStatus"`
	MemoryPressureSince    time.Time `json:"memoryPressureSince"`
	MemoryAllocatableBytes uint64    `json:"memoryAllocatableBytes"`
	MemoryAllocatableKnown bool      `json:"memoryAllocatableKnown"`
}

type NamespaceSnapshot struct {
	Namespace  string                `json:"namespace"`
	CapturedAt time.Time             `json:"capturedAt"`
	PodCount   int                   `json:"podCount"`
	Memory     model.MemoryBreakdown `json:"memory"`
}

type WorkloadSnapshot struct {
	Namespace       string                `json:"namespace"`
	Kind            string                `json:"kind"`
	Name            string                `json:"name"`
	CapturedAt      time.Time             `json:"capturedAt"`
	PodCount        int                   `json:"podCount"`
	LargestPodName  string                `json:"largestPodName"`
	LargestPodBytes uint64                `json:"largestPodBytes"`
	Pods            []PodSnapshot         `json:"pods"`
	Memory          model.MemoryBreakdown `json:"memory"`
}

type PodHistory struct {
	Namespace string               `json:"namespace"`
	PodName   string               `json:"podName"`
	PodUID    string               `json:"podUID"`
	NodeName  string               `json:"nodeName"`
	Points    []MemoryHistoryPoint `json:"points"`
}

type MemoryHistoryPoint struct {
	CapturedAt             time.Time `json:"capturedAt"`
	TotalBytes             uint64    `json:"totalBytes"`
	AnonBytes              uint64    `json:"anonBytes"`
	FileCacheBytes         uint64    `json:"fileCacheBytes"`
	ShmemBytes             uint64    `json:"shmemBytes"`
	SlabReclaimableBytes   uint64    `json:"slabReclaimableBytes"`
	SlabUnreclaimableBytes uint64    `json:"slabUnreclaimableBytes"`
	KernelBytes            uint64    `json:"kernelBytes"`
	SocketBytes            uint64    `json:"socketBytes"`
	PageTableBytes         uint64    `json:"pageTableBytes"`
	FileMappedBytes        uint64    `json:"fileMappedBytes"`
	AnonTHPBytes           uint64    `json:"anonTHPBytes"`
	FileTHPBytes           uint64    `json:"fileTHPBytes"`
	ShmemTHPBytes          uint64    `json:"shmemTHPBytes"`
	ResidualBytes          uint64    `json:"residualBytes"`
	SwapBytes              uint64    `json:"swapBytes"`
	PeakBytes              uint64    `json:"peakBytes"`
	PSISomeAvg10           float64   `json:"psiSomeAvg10"`
	PSIFullAvg10           float64   `json:"psiFullAvg10"`
	OOMEventsDelta         uint64    `json:"oomEventsDelta"`
	OOMKillEventsDelta     uint64    `json:"oomKillEventsDelta"`
	HighEventsDelta        uint64    `json:"highEventsDelta"`
	MaxEventsDelta         uint64    `json:"maxEventsDelta"`
	ReclaimDeltasKnown     bool      `json:"reclaimDeltasKnown"`
	RefaultAnonDelta       uint64    `json:"refaultAnonDelta"`
	RefaultFileDelta       uint64    `json:"refaultFileDelta"`
	PageScanDelta          uint64    `json:"pageScanDelta"`
	PageStealDelta         uint64    `json:"pageStealDelta"`
	MajorFaultsDelta       uint64    `json:"majorFaultsDelta"`
}

type IncidentBundle struct {
	SchemaVersion int                  `json:"schemaVersion"`
	CapturedAt    time.Time            `json:"capturedAt"`
	ToolVersion   string               `json:"toolVersion"`
	Redacted      bool                 `json:"redacted"`
	Pods          []PodSnapshot        `json:"pods"`
	Nodes         []NodeSnapshotStatus `json:"nodes"`
	Histories     []PodHistory         `json:"histories,omitempty"`
}

type NodeSnapshotStatus struct {
	NodeName       string          `json:"nodeName"`
	CapturedAt     time.Time       `json:"capturedAt"`
	ContainerCount int             `json:"containerCount"`
	Stale          bool            `json:"stale"`
	Environment    NodeEnvironment `json:"environment"`
}

type SnapshotPostResponse struct {
	OK         bool `json:"ok"`
	Containers int  `json:"containers"`
}

type IngestionEpoch struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Epoch             string `json:"epoch"`
	SchemaVersion     int    `json:"schemaVersion"`
	LastSequence      uint64 `json:"lastSequence"`
}

type NodeSnapshotRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	NodeUID           string        `json:"nodeUID"`
	Epoch             string        `json:"epoch"`
	Sequence          uint64        `json:"sequence"`
	Snapshot          AgentSnapshot `json:"snapshot"`
}

type NodeSnapshotResponse struct {
	metav1.TypeMeta `json:",inline"`
	Accepted        bool `json:"accepted"`
	Duplicate       bool `json:"duplicate"`
	Containers      int  `json:"containers"`
}

type DebugStore struct {
	TotalContainers  int `json:"totalContainers"`
	StaleContainers  int `json:"staleContainers"`
	NodeRecords      int `json:"nodeRecords"`
	MaxNodes         int `json:"maxNodes"`
	MaxContainers    int `json:"maxContainers"`
	MaxResponseBytes int `json:"maxResponseBytes"`
	Pods             int `json:"pods"`
	Namespaces       int `json:"namespaces"`
	HistorySeries    int `json:"historySeries"`
	HistoryPoints    int `json:"historyPoints"`
}

type StoreDebug = DebugStore

type CollectorIngestionStats struct {
	Results             map[string]uint64
	LastDurationSeconds float64
}
