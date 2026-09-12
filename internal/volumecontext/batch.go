package volumecontext

import (
	"bytes"
	"encoding/json"
	"io"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
	"k8s.io/apimachinery/pkg/util/validation"
)

// Batch holds private producer data. It is not a Node memory observation and
// must never be included in Node-only reads, diagnostics or retained history.
type Batch struct {
	nodeName   string
	nodeUID    string
	reportedAt time.Time
	state      Usage
	records    []RawUsage
}

// NewBatch validates cardinality and identity before copying pointer-rich data.
// Authenticating Node ownership and ordering is the ingestion adapter's job.
func NewBatch(nodeName, nodeUID string, at time.Time, state Usage, records []RawUsage, now time.Time) (Batch, error) {
	if !validName(nodeName) || !validUID(nodeUID) || at.IsZero() || at.Before(now.Add(-ExpireAfter)) || at.After(now.Add(FutureSkew)) ||
		len(records) > MaxBatchRecords {
		return Batch{}, ErrInvalid
	}
	if err := validateUsageState(state); err != nil {
		return Batch{}, err
	}
	if state.Availability != volumehealth.Reported && len(records) != 0 {
		return Batch{}, ErrInvalid
	}
	seen := make(map[string]bool, len(records))
	counts := make(map[string]int)
	result := Batch{nodeName: nodeName, nodeUID: nodeUID, reportedAt: at, state: state, records: make([]RawUsage, len(records))}
	for i, raw := range records {
		if len(validation.IsDNS1123Label(raw.Namespace)) != 0 || !validUID(raw.PodUID) || raw.NodeUID != nodeUID || !validName(raw.VolumeName) || len(raw.VolumeName) > 63 {
			return Batch{}, ErrInvalid
		}
		if (raw.PVCName == "" && raw.PVCNamespace != "") || (raw.PVCName != "" && (!validName(raw.PVCName) || raw.PVCNamespace != raw.Namespace)) {
			return Batch{}, ErrScope
		}
		if err := validateFilesystem(raw.Filesystem, now); err != nil {
			return Batch{}, err
		}
		if raw.Filesystem.CapturedAt.After(at.Add(FutureSkew)) || raw.Filesystem.CapturedAt.Before(now.Add(-ExpireAfter)) {
			return Batch{}, ErrInvalid
		}
		pod := raw.Namespace + "\x00" + raw.PodUID
		key := pod + "\x00" + raw.VolumeName
		counts[pod]++
		if seen[key] || counts[pod] > MaxVolumesPerPod {
			return Batch{}, ErrInvalid
		}
		seen[key] = true
		result.records[i] = raw
		result.records[i].Filesystem = *cloneFilesystem(&raw.Filesystem)
	}
	body, err := result.EncodePrivate()
	if err != nil || len(body) > MaxBatchBytes {
		return Batch{}, ErrInvalid
	}
	return result, nil
}

type batchWire struct {
	SchemaVersion int         `json:"schemaVersion"`
	NodeName      string      `json:"nodeName"`
	NodeUID       string      `json:"nodeUID"`
	ReportedAt    time.Time   `json:"reportedAt"`
	State         Usage       `json:"state"`
	Records       []usageWire `json:"records"`
}

type usageWire struct {
	Namespace    string     `json:"namespace"`
	PodUID       string     `json:"podUID"`
	VolumeName   string     `json:"volumeName"`
	PVCNamespace string     `json:"pvcNamespace,omitempty"`
	PVCName      string     `json:"pvcName,omitempty"`
	Filesystem   Filesystem `json:"filesystem"`
}

// EncodePrivate is for authenticated transport or bounded in-memory storage,
// never operator output. Batch's default JSON encoder remains aggregate-only.
func (b Batch) EncodePrivate() ([]byte, error) {
	wire := batchWire{SchemaVersion: SchemaVersion, NodeName: b.nodeName, NodeUID: b.nodeUID, ReportedAt: b.reportedAt,
		State: b.state, Records: make([]usageWire, len(b.records))}
	for i, raw := range b.records {
		wire.Records[i] = usageWire{Namespace: raw.Namespace, PodUID: raw.PodUID, VolumeName: raw.VolumeName,
			PVCNamespace: raw.PVCNamespace, PVCName: raw.PVCName, Filesystem: raw.Filesystem}
	}
	return json.Marshal(wire)
}

// DecodePrivate checks encoded bounds, duplicate keys, collection bounds and
// strict field names before allocating the typed observation slice.
func DecodePrivate(data []byte, now time.Time) (Batch, error) {
	if len(data) > MaxBatchBytes || boundedWire(data) != nil {
		return Batch{}, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var wire batchWire
	if decoder.Decode(&wire) != nil || decoder.Decode(&struct{}{}) != io.EOF || wire.SchemaVersion != SchemaVersion {
		return Batch{}, ErrInvalid
	}
	records := make([]RawUsage, len(wire.Records))
	for i, raw := range wire.Records {
		records[i] = RawUsage{Namespace: raw.Namespace, PodUID: raw.PodUID, NodeUID: wire.NodeUID, VolumeName: raw.VolumeName,
			PVCNamespace: raw.PVCNamespace, PVCName: raw.PVCName, Filesystem: raw.Filesystem}
	}
	return NewBatch(wire.NodeName, wire.NodeUID, wire.ReportedAt, wire.State, records, now)
}

func (b Batch) NodeIdentity() (string, string) { return b.nodeName, b.nodeUID }
func (b Batch) ReportedAt() time.Time          { return b.reportedAt }
func (b Batch) State() Usage                   { return b.state }
func (b Batch) Len() int                       { return len(b.records) }

// ForPod cannot grant access: the caller must supply a currently authorised
// scope. Join subsequently checks current volume and PVC bindings.
func (b Batch) ForPod(scope PodScope) []RawUsage {
	rows := []RawUsage{}
	if scope.NodeName != b.nodeName || scope.NodeUID != b.nodeUID {
		return rows
	}
	for _, raw := range b.records {
		if raw.Namespace == scope.Namespace && raw.PodUID == scope.PodUID {
			copy := raw
			copy.Filesystem = *cloneFilesystem(&raw.Filesystem)
			rows = append(rows, copy)
		}
	}
	return rows
}

func (b Batch) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Records      int                       `json:"records"`
		Availability volumehealth.Availability `json:"availability"`
	}{len(b.records), b.state.Availability})
}
func (Batch) String() string   { return "private volume batch" }
func (Batch) GoString() string { return "private volume batch" }
