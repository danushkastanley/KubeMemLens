package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"sigs.k8s.io/yaml"
)

type explanationDocument struct {
	SchemaVersion int                 `json:"schemaVersion"`
	GeneratedAt   time.Time           `json:"generatedAt"`
	Target        explanationTarget   `json:"target"`
	Memory        memoryEvidence      `json:"memory"`
	Finding       findingEvidence     `json:"finding"`
	Kubernetes    *kubernetesEvidence `json:"kubernetes,omitempty"`
	Containers    []containerEvidence `json:"containers,omitempty"`
	Replicas      []replicaEvidence   `json:"replicas,omitempty"`
	NextCommands  []string            `json:"nextCommands"`
}

type explanationTarget struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

type findingEvidence struct {
	Diagnosis         explain.Diagnosis  `json:"diagnosis"`
	Severity          explain.Severity   `json:"severity"`
	Confidence        explain.Confidence `json:"confidence"`
	ConfidenceReason  string             `json:"confidenceReason"`
	Summary           string             `json:"summary"`
	LikelyExplanation string             `json:"likelyExplanation"`
	Evidence          []string           `json:"evidence"`
	SuggestedChecks   []string           `json:"suggestedChecks"`
	Caveats           []string           `json:"caveats"`
	EvidenceWindow    evidenceWindow     `json:"evidenceWindow"`
}

type evidenceWindow struct {
	ObservationStart *time.Time `json:"observationStart,omitempty"`
	ObservationEnd   *time.Time `json:"observationEnd,omitempty"`
	DeltaStart       *time.Time `json:"counterDeltaStart,omitempty"`
	DeltaEnd         *time.Time `json:"counterDeltaEnd,omitempty"`
	DeltaKnown       bool       `json:"counterDeltaKnown"`
	DeltaComplete    bool       `json:"counterDeltaComplete"`
	DeltaUniform     bool       `json:"counterDeltaUniform"`
	DeltaWindowCount int        `json:"counterDeltaWindowCount"`
}

type memoryEvidence struct {
	TotalBytes             uint64           `json:"totalBytes"`
	AnonBytes              uint64           `json:"anonBytes"`
	FileCacheBytes         uint64           `json:"fileCacheBytes"`
	ShmemBytes             uint64           `json:"shmemBytes"`
	SlabBytes              uint64           `json:"slabBytes"`
	SlabReclaimableBytes   uint64           `json:"slabReclaimableBytes"`
	SlabUnreclaimableBytes uint64           `json:"slabUnreclaimableBytes"`
	KernelBytes            uint64           `json:"kernelBytes"`
	KernelOtherBytes       uint64           `json:"kernelOtherBytes"`
	SocketBytes            uint64           `json:"socketBytes"`
	PageTableBytes         uint64           `json:"pageTableBytes"`
	FileMappedBytes        uint64           `json:"fileMappedBytes"`
	AnonTHPBytes           uint64           `json:"anonTHPBytes"`
	FileTHPBytes           uint64           `json:"fileTHPBytes"`
	ShmemTHPBytes          uint64           `json:"shmemTHPBytes"`
	ResidualBytes          uint64           `json:"residualBytes"`
	DirtyBytes             uint64           `json:"dirtyBytes"`
	WritebackBytes         uint64           `json:"writebackBytes"`
	SwapBytes              uint64           `json:"swapBytes"`
	Peak                   boundaryEvidence `json:"peak"`
	Limit                  boundaryEvidence `json:"limit"`
	Pressure               pressureEvidence `json:"pressure"`
	RecentEvents           eventEvidence    `json:"recentEvents"`
	RecentReclaim          reclaimEvidence  `json:"recentReclaim"`
}

type boundaryEvidence struct {
	Known     bool   `json:"known"`
	Unlimited bool   `json:"unlimited"`
	Bytes     uint64 `json:"bytes"`
}

type pressureEvidence struct {
	Known     bool    `json:"known"`
	SomeAvg10 float64 `json:"someAvg10"`
	FullAvg10 float64 `json:"fullAvg10"`
}

type eventEvidence struct {
	OOM     uint64 `json:"oom"`
	OOMKill uint64 `json:"oomKill"`
	High    uint64 `json:"high"`
	Max     uint64 `json:"max"`
}

type reclaimEvidence struct {
	Known       bool   `json:"known"`
	RefaultAnon uint64 `json:"refaultAnon"`
	RefaultFile uint64 `json:"refaultFile"`
	PageScan    uint64 `json:"pageScan"`
	PageSteal   uint64 `json:"pageSteal"`
	MajorFaults uint64 `json:"majorFaults"`
}

type kubernetesEvidence struct {
	Phase                 string     `json:"phase"`
	QoSClass              string     `json:"qosClass"`
	Node                  string     `json:"node"`
	NodeMemoryPressure    string     `json:"nodeMemoryPressure"`
	WorkloadKind          string     `json:"workloadKind"`
	WorkloadName          string     `json:"workloadName"`
	RestartCount          int32      `json:"restartCount"`
	LastTerminationReason string     `json:"lastTerminationReason,omitempty"`
	LastTerminationAt     *time.Time `json:"lastTerminationAt,omitempty"`
	MemoryRequestBytes    uint64     `json:"memoryRequestBytes"`
	MemoryLimitBytes      uint64     `json:"memoryLimitBytes"`
	NodeMemoryAllocatable uint64     `json:"nodeMemoryAllocatable"`
	RuntimeClassName      string     `json:"runtimeClassName"`
	MemoryEmptyDirCount   int        `json:"memoryEmptyDirCount"`
	MemoryEmptyDirLimited int        `json:"memoryEmptyDirLimited"`
	MemoryEmptyDirLimits  uint64     `json:"memoryEmptyDirLimitBytes"`
}

type containerEvidence struct {
	Name    string          `json:"name"`
	Memory  memoryEvidence  `json:"memory"`
	Finding findingEvidence `json:"finding"`
}

type replicaEvidence struct {
	PodName string          `json:"podName"`
	Node    string          `json:"node"`
	Memory  memoryEvidence  `json:"memory"`
	Finding findingEvidence `json:"finding"`
}

func podExplanationDocument(pod api.PodSnapshot) explanationDocument {
	result := explain.AnalyzePod(pod)
	document := explanationDocument{
		SchemaVersion: api.CurrentExplanationSchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Target:        explanationTarget{Kind: "Pod", Namespace: pod.Namespace, Name: pod.PodName},
		Memory:        memoryOutput(pod.Memory),
		Finding:       findingOutput(result),
		Kubernetes: &kubernetesEvidence{
			Phase: pod.Context.Phase, QoSClass: pod.Context.QoSClass, Node: pod.NodeName,
			NodeMemoryPressure: pod.Context.NodeMemoryPressure,
			WorkloadKind:       pod.Context.WorkloadKind, WorkloadName: pod.Context.WorkloadName,
			RestartCount: pod.Context.RestartCount, LastTerminationReason: pod.Context.LastTerminationReason,
			LastTerminationAt:  optionalTime(pod.Context.LastTerminationKnown, pod.Context.LastTerminationFinishedAt),
			MemoryRequestBytes: pod.Context.MemoryRequestBytes, MemoryLimitBytes: pod.Context.MemoryLimitBytes,
			NodeMemoryAllocatable: pod.Context.NodeMemoryAllocatable,
			RuntimeClassName:      pod.Context.RuntimeClassName,
			MemoryEmptyDirCount:   pod.Context.MemoryEmptyDirCount,
			MemoryEmptyDirLimited: pod.Context.MemoryEmptyDirLimited,
			MemoryEmptyDirLimits:  pod.Context.MemoryEmptyDirLimitBytes,
		},
		NextCommands: podNextCommands(pod),
	}
	for _, container := range pod.Containers {
		document.Containers = append(document.Containers, containerEvidence{Name: container.ContainerName, Memory: memoryOutput(container.Memory), Finding: findingOutput(explain.AnalyzeContainer(container))})
	}
	return document
}

func optionalTime(known bool, value time.Time) *time.Time {
	if !known || value.IsZero() {
		return nil
	}
	return &value
}

func workloadExplanationDocument(workload api.WorkloadSnapshot) explanationDocument {
	document := explanationDocument{
		SchemaVersion: api.CurrentExplanationSchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Target:        explanationTarget{Kind: workload.Kind, Namespace: workload.Namespace, Name: workload.Name},
		Memory:        memoryOutput(workload.Memory),
		Finding:       findingOutput(explain.AnalyzeWorkload(workload)),
		NextCommands:  workloadNextCommands(workload),
	}
	for _, pod := range workload.Pods {
		document.Replicas = append(document.Replicas, replicaEvidence{PodName: pod.PodName, Node: pod.NodeName, Memory: memoryOutput(pod.Memory), Finding: findingOutput(explain.AnalyzePod(pod))})
	}
	return document
}

func memoryOutput(memory model.MemoryBreakdown) memoryEvidence {
	oom, oomKill, high, maxEvents := memory.RecentEventCounts()
	return memoryEvidence{
		TotalBytes: memory.TotalBytes, AnonBytes: memory.RSSBytes(), FileCacheBytes: memory.CacheBytes(),
		ShmemBytes: memory.ShmemBytes, SlabBytes: memory.SlabBytes, KernelBytes: memory.KernelBytes,
		SlabReclaimableBytes:   memory.SlabReclaimableBytes,
		SlabUnreclaimableBytes: memory.SlabUnreclaimableBytes,
		KernelOtherBytes:       memory.KernelOtherBytes(), SocketBytes: memory.SocketBytes,
		PageTableBytes: memory.PageTableBytes, FileMappedBytes: memory.FileMappedBytes,
		AnonTHPBytes: memory.AnonTHPBytes, FileTHPBytes: memory.FileTHPBytes, ShmemTHPBytes: memory.ShmemTHPBytes,
		ResidualBytes: memory.ResidualBytes(),
		DirtyBytes:    memory.DirtyBytes, WritebackBytes: memory.WritebackBytes, SwapBytes: memory.SwapCurrentBytes,
		Peak:         boundaryEvidence{Known: memory.PeakKnown, Bytes: memory.PeakBytes},
		Limit:        boundaryEvidence{Known: memory.MaxKnown, Unlimited: memory.MaxUnlimited, Bytes: memory.MaxBytes},
		Pressure:     pressureEvidence{Known: memory.PressureKnown, SomeAvg10: memory.PSISomeAvg10, FullAvg10: memory.PSIFullAvg10},
		RecentEvents: eventEvidence{OOM: oom, OOMKill: oomKill, High: high, Max: maxEvents},
		RecentReclaim: reclaimEvidence{
			Known: memory.ReclaimDeltasKnown, RefaultAnon: memory.RefaultAnonDelta,
			RefaultFile: memory.RefaultFileDelta, PageScan: memory.PageScanDelta,
			PageSteal: memory.PageStealDelta, MajorFaults: memory.MajorPageFaultsDelta,
		},
	}
}

func findingOutput(result explain.Result) findingEvidence {
	return findingEvidence{
		Diagnosis: result.Diagnosis, Severity: result.Severity, Confidence: result.Confidence, ConfidenceReason: result.ConfidenceReason,
		Summary: result.Summary, LikelyExplanation: result.LikelyExplanation,
		Evidence: append([]string(nil), result.Signals...), SuggestedChecks: append([]string(nil), result.SuggestedChecks...),
		Caveats: append([]string(nil), result.Caveats...), EvidenceWindow: evidenceWindowOutput(result.EvidenceWindow),
	}
}

func evidenceWindowOutput(window explain.EvidenceWindow) evidenceWindow {
	return evidenceWindow{
		ObservationStart: optionalTimestamp(window.ObservationStart),
		ObservationEnd:   optionalTimestamp(window.ObservationEnd),
		DeltaStart:       optionalTimestamp(window.DeltaStart),
		DeltaEnd:         optionalTimestamp(window.DeltaEnd),
		DeltaKnown:       window.DeltaKnown,
		DeltaComplete:    window.DeltaComplete,
		DeltaUniform:     window.DeltaUniform,
		DeltaWindowCount: window.DeltaWindowCount,
	}
}

func optionalTimestamp(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	value = value.UTC()
	return &value
}

func writeExplanationDocument(w io.Writer, output string, document explanationDocument) error {
	body, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	switch output {
	case "json":
		_, err = fmt.Fprintln(w, string(body))
	case "yaml":
		body, err = yaml.JSONToYAML(body)
		if err == nil {
			_, err = w.Write(body)
		}
	default:
		return fmt.Errorf("invalid output %q, want text, json, or yaml", output)
	}
	return err
}
