package recommend

import (
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/explain"
)

func TestVolumeRecommendationsAreGuardedAndBounded(t *testing.T) {
	result := explain.VolumeResult{}
	for _, kind := range []explain.VolumeSignalKind{explain.VolumeFilesystemPressure, explain.VolumeInodePressure, explain.VolumeAdverseHealth, explain.VolumeTmpfsContext, explain.VolumeSharedMemory, explain.VolumeWriteback, explain.VolumeIOStall} {
		result.Signals = append(result.Signals, explain.VolumeSignal{Kind: kind, Summary: "private-backend"})
	}
	items := ForVolumes(result)
	if len(items) != 4 {
		t.Fatalf("unexpected recommendation count %d", len(items))
	}
	for _, item := range items {
		if item.Priority != "investigate" || len(item.Conditions) < 2 || strings.Contains(item.Action+item.Rationale, "private-backend") {
			t.Fatal("unguarded or private recommendation")
		}
	}
	if len(ForVolumes(explain.VolumeResult{})) != 0 {
		t.Fatal("missing evidence generated recommendations")
	}
}
