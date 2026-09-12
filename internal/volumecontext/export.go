package volumecontext

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

// View is an explicitly authorised wire DTO, not the default report encoder.
// No UID, path, backend handle or upstream message is exposed.
type View struct {
	SchemaVersion int           `json:"schemaVersion"`
	Namespace     string        `json:"namespace"`
	PodName       string        `json:"podName"`
	Volumes       []NamedVolume `json:"volumes"`
}

type NamedVolume struct {
	EvidenceID    string        `json:"evidenceID,omitempty"`
	FilesystemID  string        `json:"filesystemID,omitempty"`
	VolumeName    string        `json:"volumeName"`
	PVCName       string        `json:"pvcName,omitempty"`
	Driver        string        `json:"driver,omitempty"`
	Configuration Configuration `json:"configuration"`
	Usage         Usage         `json:"usage"`
	Health        []Health      `json:"health"`
}

type Health struct {
	HealthReport
	LastGood *HealthReport `json:"lastGood,omitempty"`
}

type HealthReport struct {
	Observation  volumehealth.Observation `json:"observation"`
	TransitionAt time.Time                `json:"transitionAt,omitzero"`
	Conditions   []Condition              `json:"conditions,omitempty"`
}

type Condition struct {
	Status       volumehealth.Status `json:"status"`
	Reason       string              `json:"reason,omitempty"`
	TransitionAt time.Time           `json:"transitionAt,omitzero"`
	AccessMode   string              `json:"accessMode,omitempty"`
	VolumeMode   string              `json:"volumeMode,omitempty"`
}

// Authorised returns an isolated response. Mutating it cannot change the
// retained report. Only invoke after the current request's authorisation.
func (r Report) Authorised() View {
	view := View{SchemaVersion: SchemaVersion, Namespace: r.scope.Namespace, PodName: r.scope.PodName,
		Volumes: make([]NamedVolume, len(r.volumes))}
	for i, row := range r.volumes {
		binding := cloneBinding(row.Binding)
		usage := row.Usage
		usage.Filesystem = cloneFilesystem(usage.Filesystem)
		usage.LastGood = cloneFilesystem(usage.LastGood)
		value := NamedVolume{VolumeName: binding.VolumeName, PVCName: binding.PVCName, Driver: binding.Driver,
			Configuration: binding.Configuration, Usage: usage, Health: make([]Health, len(row.Health))}
		value.EvidenceID, value.FilesystemID = evidenceKeys(r.scope, binding)
		for j, input := range row.Health {
			h := Health{HealthReport: namedHealth(input.Observation)}
			if input.LastGood != nil {
				last := namedHealth(*input.LastGood)
				h.LastGood = &last
			}
			value.Health[j] = h
		}
		view.Volumes[i] = value
	}
	return view
}

// RedactedVolume retains source evidence without names or free-form text.
// Its position is not an identity that can be joined across captures.
type RedactedVolume struct {
	Configuration Configuration    `json:"configuration"`
	Usage         Usage            `json:"usage"`
	Health        []RedactedHealth `json:"health"`
}

type RedactedHealth struct {
	volumehealth.Observation
	LastGood *volumehealth.Observation `json:"lastGood,omitempty"`
}

type Redacted struct {
	SchemaVersion int              `json:"schemaVersion"`
	Volumes       []RedactedVolume `json:"volumes"`
}

func (r Report) Redacted() Redacted {
	view := r.Authorised()
	result := Redacted{SchemaVersion: SchemaVersion, Volumes: make([]RedactedVolume, len(view.Volumes))}
	for i, row := range view.Volumes {
		value := RedactedVolume{Configuration: row.Configuration, Usage: row.Usage, Health: make([]RedactedHealth, len(row.Health))}
		for j, health := range row.Health {
			value.Health[j].Observation = health.Observation
			if health.LastGood != nil {
				last := health.LastGood.Observation
				value.Health[j].LastGood = &last
			}
		}
		result.Volumes[i] = value
	}
	return result
}

// Summary has fixed-cardinality keys. It contains no per-volume identifiers,
// arbitrary condition status strings, reasons or upstream text.
type Summary struct {
	Volumes        int `json:"volumes"`
	UsageReported  int `json:"usageReported"`
	UsageStale     int `json:"usageStale"`
	AdverseReports int `json:"adverseReports"`
	UnknownReports int `json:"unknownReports"`
}

func (r Report) Summary() Summary {
	result := Summary{Volumes: len(r.volumes)}
	for _, row := range r.volumes {
		if row.Usage.Filesystem != nil {
			result.UsageReported++
		}
		if row.Usage.Freshness == volumehealth.Stale {
			result.UsageStale++
		}
		for _, h := range row.Health {
			if h.Adverse || (h.LastGood != nil && h.LastGood.Adverse) {
				result.AdverseReports++
			}
			if h.UnknownStatus || h.State == volumehealth.StateUnknown || (h.LastGood != nil && h.LastGood.UnknownStatus) {
				result.UnknownReports++
			}
		}
	}
	return result
}

func (r Report) MarshalJSON() ([]byte, error) { return json.Marshal(r.Redacted()) }
func (r Report) String() string               { return fmt.Sprint(r.Summary()) }
func (r Report) GoString() string             { return r.String() }
