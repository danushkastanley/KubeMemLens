package incident

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/model"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"k8s.io/apimachinery/pkg/util/validation"
)

func ValidateVolume(b VolumeBundle) error {
	invalid := fmt.Errorf("invalid volume incident schema, scope or evidence")
	p := b.Pod
	if b.SchemaVersion != VolumeSchemaVersion || b.CapturedAt.IsZero() || b.ToolVersion == "" || len(b.ToolVersion) > 512 || len(b.Caveats) > 16 || len(p.Containers) > 256 || p.CapturedAt.IsZero() || p.CapturedAt.After(b.CapturedAt.Add(volumecontext.FutureSkew)) || len(validation.IsDNS1123Label(p.Namespace)) != 0 || len(validation.IsDNS1123Subdomain(p.PodName)) != 0 || p.Namespace != b.Volumes.Namespace || p.PodName != b.Volumes.PodName {
		return invalid
	}
	if err := validateScalars(reflect.ValueOf(b)); err != nil {
		return invalid
	}
	for _, caveat := range b.Caveats {
		switch caveat {
		case "Filesystem values are separate from memory charge; correlation does not prove causality.", "Captured history covers only the selected Pod and Node instance.", "Aliases apply only within this capture; they cannot establish identity continuity across captures.", "Current-instance history was not reported.":
		default:
			return invalid
		}
	}
	if volumecontext.ValidateView(b.Volumes, b.CapturedAt) != nil || !validVolumeMemory(p.Memory) || (model.ContainerMemoryResources{Pod: p.Context.Resources}).Validate() != nil {
		return invalid
	}
	if !b.Redacted && (p.PodUID == "" || len(p.PodUID) > 128) {
		return invalid
	}
	seen := map[string]bool{}
	for _, c := range p.Containers {
		if c.Namespace != p.Namespace || c.PodName != p.PodName || c.PodUID != p.PodUID || c.NodeName != p.NodeName || c.ContainerName == "" || seen[c.ContainerName] || c.CapturedAt.IsZero() || c.CapturedAt.After(b.CapturedAt.Add(volumecontext.FutureSkew)) || !validVolumeMemory(c.Memory) || c.Context.Resources.Validate() != nil {
			return invalid
		}
		seen[c.ContainerName] = true
	}
	if h := b.History; h != nil {
		if h.Namespace != p.Namespace || h.PodName != p.PodName || h.PodUID != p.PodUID || h.NodeName != p.NodeName || len(h.Points) > MaxVolumeHistoryPoints {
			return invalid
		}
		var before time.Time
		for _, point := range h.Points {
			if point.CapturedAt.IsZero() || point.CapturedAt.After(b.CapturedAt.Add(volumecontext.FutureSkew)) || (!before.IsZero() && !point.CapturedAt.After(before)) || !validVolumePercent(point.PSISomeAvg10) || !validVolumePercent(point.PSIFullAvg10) {
				return invalid
			}
			before = point.CapturedAt
		}
	}
	body, err := json.Marshal(b)
	if err != nil || len(body) > MaxVolumeBytes {
		return invalid
	}
	if b.Redacted {
		var copy VolumeBundle
		if json.Unmarshal(body, &copy) != nil {
			return invalid
		}
		redactVolume(&copy)
		if !reflect.DeepEqual(copy, b) {
			return fmt.Errorf("redacted volume incident contains private identity or text")
		}
	}
	return nil
}

func validVolumeMemory(m model.MemoryBreakdown) bool {
	if m.IOPressure.Validate() != nil {
		return false
	}
	for _, value := range []float64{m.PSISomeAvg10, m.PSISomeAvg60, m.PSISomeAvg300, m.PSIFullAvg10, m.PSIFullAvg60, m.PSIFullAvg300} {
		if !validVolumePercent(value) {
			return false
		}
	}
	return true
}

func validVolumePercent(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 100
}
