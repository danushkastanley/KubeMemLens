package explain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

func comparisonInputs(t *testing.T) (VolumeInput, VolumeInput) {
	t.Helper()
	before := volumeInputFixture(t)
	before.Volumes.Volumes[0].EvidenceID = strings.Repeat("a", 64)
	before.Volumes.Volumes[0].FilesystemID = strings.Repeat("b", 64)
	body, _ := json.Marshal(before)
	var after VolumeInput
	if json.Unmarshal(body, &after) != nil {
		t.Fatal("clone")
	}
	after.Now = before.Now.Add(10 * time.Second)
	after.Pod.CapturedAt = after.Now
	after.Pod.Containers[0].CapturedAt = after.Now
	after.Volumes.Volumes[0].Usage.Filesystem.CapturedAt = after.Now
	after.Pod.Memory.TotalBytes += 10
	return before, after
}

func TestVolumeComparisonRequiresIdentityAndSourceContinuity(t *testing.T) {
	before, after := comparisonInputs(t)
	result, err := CompareVolumes(before, after)
	if err != nil || !result.MemoryComparable || len(result.Rows) != 1 || !result.Rows[0].UsageComparable {
		t.Fatalf("valid comparison: %+v %v", result, err)
	}
	after.Pod.Containers[0].ContainerID = "replacement"
	result, _ = CompareVolumes(before, after)
	if result.MemoryComparable || !result.Rows[0].UsageComparable {
		t.Fatal("memory and filesystem continuity conflated")
	}
	after.Volumes.Volumes[0].Usage.Freshness = volumehealth.Stale
	result, _ = CompareVolumes(before, after)
	if result.Rows[0].UsageComparable {
		t.Fatal("stale usage produced a delta")
	}
	before, after = comparisonInputs(t)
	after.Now = before.Now.Add(-time.Second)
	if _, err := CompareVolumes(before, after); err == nil {
		t.Fatal("reversed captures accepted")
	}
}

func TestRedactedAliasesCannotEstablishComparisonIdentity(t *testing.T) {
	before, after := comparisonInputs(t)
	before.Pod.PodUID, after.Pod.PodUID = "", ""
	for _, input := range []*VolumeInput{&before, &after} {
		input.Volumes.Volumes[0].EvidenceID = ""
		input.Volumes.Volumes[0].FilesystemID = ""
	}
	result, err := CompareVolumes(before, after)
	if err != nil || result.MemoryComparable || len(result.Rows) != 0 {
		t.Fatal("names or aliases substituted for identity")
	}
	if !strings.Contains(strings.Join(result.Caveats, " "), "aliases are not identities") {
		t.Fatal("missing alias caveat")
	}
}

func TestPartiallyLinkedVolumeComparisonRetainsUnmatchedEvidence(t *testing.T) {
	before, after := comparisonInputs(t)
	extra := before.Volumes.Volumes[0]
	extra.VolumeName = "old-only"
	extra.EvidenceID, extra.FilesystemID = "", ""
	before.Volumes.Volumes = append(before.Volumes.Volumes, extra)
	extra.VolumeName = "new-only"
	after.Volumes.Volumes = append(after.Volumes.Volumes, extra)
	result, err := CompareVolumes(before, after)
	if err != nil || len(result.Rows) != 1 || len(result.UnmatchedBefore) != 1 || len(result.UnmatchedAfter) != 1 || result.UnmatchedBefore[0] != 1 || result.UnmatchedAfter[0] != 1 {
		t.Fatal("partial links lost unmatched evidence", err, result)
	}
}
