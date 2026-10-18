package nodestats

import (
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

func TestExpiredVolumeSourceDoesNotDiscardAnotherPodsCurrentMeasurement(t *testing.T) {
	expiredValues := strings.Replace(volumeValues, "2026-09-11T12:00:00Z", "2026-09-11T11:57:59Z", 1)
	body := withVolumePods(`[{` + podReference + `,"volume":[{"name":"data",` + volumeValues + `}]},{"podRef":{"namespace":"tenant-b","uid":"expired-pod"},"volume":[{"name":"expired",` + expiredValues + `}]}]`)
	source, requests := volumeHarness(t, body, VolumeStatsEnabled)
	sample, err := source.ReadSample(t.Context())
	if err != nil || requests.Load() != 1 || sample.Node.Availability != capability.Available || sample.Volumes == nil || sample.Volumes.State().Availability != volumehealth.Reported || sample.Volumes.Len() != 1 {
		t.Fatal("expired sibling discarded current source or changed acquisition", err)
	}
	scope := volumecontext.PodScope{Namespace: "tenant-a", PodUID: "pod-uid", NodeName: "node-a", NodeUID: "node-uid-a"}
	rows := sample.Volumes.ForPod(scope)
	if len(rows) != 1 || !rows[0].Filesystem.CapturedAt.Equal(sampleNow) || rows[0].Filesystem.UsedBytes == nil || *rows[0].Filesystem.UsedBytes != 0 {
		t.Fatal("current source identity, clock or zero value changed")
	}
	scope.Namespace, scope.PodUID = "tenant-b", "expired-pod"
	if len(sample.Volumes.ForPod(scope)) != 0 {
		t.Fatal("expired source survived acquisition")
	}
	metrics := source.opts.Telemetry.Render()
	for _, line := range []string{"kubememlens_volume_stats_records_total 1\n", "kubememlens_volume_stats_omissions_total 1\n", "kubememlens_volume_stats_errors_total 0\n"} {
		if !strings.Contains(metrics, line) {
			t.Fatal("expired omission was not counted independently", line)
		}
	}
}

func TestMalformedExpiredSourceStillFailsTheWholeVolumeAcquisition(t *testing.T) {
	for name, fields := range map[string]string{
		"negative":      `"time":"2026-09-11T11:57:59Z","usedBytes":-1`,
		"claim scope":   `"time":"2026-09-11T11:57:59Z","usedBytes":0,"pvcRef":{"name":"claim","namespace":"wrong-tenant"}`,
		"missing clock": `"usedBytes":0`,
	} {
		t.Run(name, func(t *testing.T) {
			body := withVolumePods(`[{` + podReference + `,"volume":[{"name":"data",` + volumeValues + `},{"name":"bad",` + fields + `}]}]`)
			source, _ := volumeHarness(t, body, VolumeStatsEnabled)
			sample, err := source.ReadSample(t.Context())
			if err != nil || sample.Node.Availability != capability.Available || sample.Volumes == nil || sample.Volumes.State().Availability != volumehealth.Unavailable || sample.Volumes.Len() != 0 {
				t.Fatal("malformed source became an accepted partial batch", err)
			}
		})
	}
}
