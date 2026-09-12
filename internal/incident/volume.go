package incident

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
)

const MaxVolumeBytes = 2 << 20
const MaxVolumeHistoryPoints = 181

type VolumeBundle struct {
	SchemaVersion int                `json:"schemaVersion"`
	CapturedAt    time.Time          `json:"capturedAt"`
	ToolVersion   string             `json:"toolVersion"`
	Redacted      bool               `json:"redacted"`
	Pod           api.PodSnapshot    `json:"pod"`
	Volumes       volumecontext.View `json:"volumes"`
	History       *api.PodHistory    `json:"history,omitempty"`
	Caveats       []string           `json:"caveats,omitempty"`
}

type VolumeCaptureReader interface {
	client.PodVolumeReader
	PodHistory(context.Context, string, string) ([]api.PodHistory, error)
}

type VolumeCaptureOptions struct {
	ExpectedUID      string
	IncludeHistory   bool
	IncludeSensitive bool
	ToolVersion      string
}

// CollectVolume repeats the authorised read for every export and overwrite
// attempt. History is read first; the final volume query rechecks the live Pod.
func CollectVolume(ctx context.Context, reader VolumeCaptureReader, namespace, name string, opts VolumeCaptureOptions) (VolumeBundle, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var histories []api.PodHistory
	if opts.IncludeHistory {
		var err error
		histories, err = reader.PodHistory(ctx, namespace, name)
		if err != nil {
			return VolumeBundle{}, err
		}
	}
	pod, volumes, err := client.ReadPodVolumeEvidence(ctx, reader, namespace, name, opts.ExpectedUID)
	if err != nil {
		return VolumeBundle{}, err
	}
	var history *api.PodHistory
	if len(histories) > 20 {
		return VolumeBundle{}, fmt.Errorf("volume capture history exceeds its series bound")
	}
	for _, candidate := range histories {
		if candidate.Namespace == namespace && candidate.PodName == name && candidate.PodUID == pod.PodUID && candidate.NodeName == pod.NodeName {
			if history != nil {
				return VolumeBundle{}, fmt.Errorf("ambiguous history for the selected Pod instance")
			}
			copy := candidate
			history = &copy
		}
	}
	b, err := NewVolume(pod, volumes, history, opts.ToolVersion, time.Now().UTC(), opts.IncludeSensitive)
	if err == nil && opts.IncludeHistory && history == nil {
		b.Caveats = append(b.Caveats, "Current-instance history was not reported.")
	}
	return b, err
}

func NewVolume(pod api.PodSnapshot, volumes api.PodVolumeContext, history *api.PodHistory, version string, at time.Time, sensitive bool) (VolumeBundle, error) {
	if pod.PodUID == "" || string(volumes.UID) != pod.PodUID || volumes.Namespace != pod.Namespace || volumes.Name != pod.PodName {
		return VolumeBundle{}, fmt.Errorf("volume capture requires matching authorised Pod instances")
	}
	b := VolumeBundle{SchemaVersion: VolumeSchemaVersion, CapturedAt: at.UTC(), ToolVersion: version, Pod: pod, Volumes: volumes.Context, History: history, Caveats: []string{"Filesystem values are separate from memory charge; correlation does not prove causality.", "Captured history covers only the selected Pod and Node instance."}}
	if err := ValidateVolume(b); err != nil {
		return VolumeBundle{}, err
	}
	body, err := json.Marshal(b)
	if err != nil {
		return VolumeBundle{}, err
	}
	var copy VolumeBundle
	if err := json.Unmarshal(body, &copy); err != nil {
		return VolumeBundle{}, err
	}
	if !sensitive {
		redactVolume(&copy)
		copy.Caveats = append(copy.Caveats, "Aliases apply only within this capture; they cannot establish identity continuity across captures.")
	}
	return copy, ValidateVolume(copy)
}

func WriteVolume(stdout io.Writer, output string, overwrite bool, bundle VolumeBundle) error {
	if err := ValidateVolume(bundle); err != nil {
		return err
	}
	encoder := json.NewEncoder(&boundedWriter{destination: io.Discard, remaining: MaxVolumeBytes})
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(bundle); err != nil {
		return fmt.Errorf("volume incident exceeds its file byte limit")
	}
	return writeDocument(stdout, output, overwrite, bundle)
}

func LegacyVolume(bundle VolumeBundle, schema int) (api.IncidentBundle, error) {
	if err := ValidateVolume(bundle); err != nil {
		return api.IncidentBundle{}, err
	}
	if schema != 1 && schema != 2 {
		return api.IncidentBundle{}, fmt.Errorf("legacy volume export supports incident schema 1 or 2")
	}
	result := api.IncidentBundle{SchemaVersion: 2, CapturedAt: bundle.CapturedAt, ToolVersion: bundle.ToolVersion, Redacted: bundle.Redacted, Partial: true, Pods: []api.PodSnapshot{bundle.Pod}, Caveats: append([]string(nil), bundle.Caveats...)}
	if bundle.History != nil {
		result.Histories = []api.PodHistory{*bundle.History}
	}
	result.Caveats = append(result.Caveats, "Volume configuration, filesystem usage, health and I/O pressure were omitted for legacy export.")
	result = api.WithoutIOIncident(result)
	if schema == 1 {
		result = api.LegacyIncident(result)
	}
	return result, nil
}
