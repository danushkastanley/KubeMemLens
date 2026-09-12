package nodestats

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

const volumeValues = `"time":"2026-09-11T12:00:00Z","capacityBytes":100,"usedBytes":0,"availableBytes":90,"inodes":1000,"inodesUsed":5`
const podReference = `"podRef":{"name":"private-pod","namespace":"tenant-a","uid":"pod-uid"}`

func withVolumePods(pods string) string {
	body := summaryBody()
	return body[:strings.Index(body, `,"pods":`)] + `,"pods":` + pods + `}`
}

func volumeHarness(t *testing.T, body string, mode VolumeStatsMode) (*Source, *atomic.Int32) {
	t.Helper()
	requests := &atomic.Int32{}
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet || r.URL.RequestURI() != "/stats/summary" || r.Header.Get("Authorization") != "Bearer node-credential" {
			t.Error("volume collection changed kubelet transport")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, body)
	})
	opts := h.opts
	opts.VolumeStats = mode
	source, err := New(h.config, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(source.Close)
	return source, requests
}

func TestVolumesUseOneSummaryRequestAndKeepNodeOutputPrivate(t *testing.T) {
	body := withVolumePods(`[{` + podReference + `,"volume":[{"name":"data","pvcRef":{"name":"private-claim","namespace":"tenant-a"},` + volumeValues + `},{"name":"inline",` + volumeValues + `}]}]`)
	source, requests := volumeHarness(t, body, VolumeStatsEnabled)
	sample, err := source.ReadSample(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 || sample.Node.Availability != capability.Available || sample.Volumes == nil || sample.Volumes.Len() != 2 {
		t.Fatal("volume enrichment changed acquisition")
	}
	scope := volumecontext.PodScope{Namespace: "tenant-a", PodUID: "pod-uid", NodeName: "node-a", NodeUID: "node-uid-a"}
	rows := sample.Volumes.ForPod(scope)
	if len(rows) != 2 || rows[0].PVCName != "private-claim" || rows[1].PVCName != "" || rows[0].Filesystem.UsedBytes == nil || *rows[0].Filesystem.UsedBytes != 0 || rows[0].Filesystem.InodesFree != nil || !rows[0].Filesystem.CapturedAt.Equal(sampleNow) {
		t.Fatal("volume source values changed")
	}
	encoded, err := json.Marshal(sample)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private-claim", "private-pod", "tenant-a", "pod-uid", `"volumeName"`} {
		if strings.Contains(string(encoded), secret) || strings.Contains(source.opts.Telemetry.Render(), secret) {
			t.Fatal("default sample or metrics leaked tenant context")
		}
	}
	metrics := source.opts.Telemetry.Render()
	if !strings.Contains(metrics, "kubememlens_volume_stats_records_total 2\n") || !strings.Contains(metrics, "kubememlens_volume_stats_errors_total 0\n") {
		t.Fatal("volume counters missing")
	}
}

func TestMalformedVolumeEnrichmentCannotEraseNodeMemory(t *testing.T) {
	for name, volume := range map[string]string{
		"negative":            `{"name":"data","time":"2026-09-11T12:00:00Z","usedBytes":-1}`,
		"fraction":            `{"name":"data","time":"2026-09-11T12:00:00Z","usedBytes":0.5}`,
		"overflow":            `{"name":"data","time":"2026-09-11T12:00:00Z","usedBytes":18446744073709551616}`,
		"missing timestamp":   `{"name":"data","usedBytes":0}`,
		"future timestamp":    `{"name":"data","time":"2026-09-11T12:01:00Z","usedBytes":0}`,
		"duplicate field":     `{"name":"data","name":"other",` + volumeValues + `}`,
		"cross-namespace PVC": `{"name":"data","pvcRef":{"name":"private-claim","namespace":"tenant-b"},` + volumeValues + `}`,
		"bad name":            `{"name":"../secret",` + volumeValues + `}`,
	} {
		t.Run(name, func(t *testing.T) {
			source, _ := volumeHarness(t, withVolumePods(`[{`+podReference+`,"volume":[`+volume+`]}]`), VolumeStatsEnabled)
			sample, err := source.ReadSample(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if sample.Node.Availability != capability.Available || sample.Node.Stats == nil || sample.Volumes == nil || sample.Volumes.State().Availability != volumehealth.Unavailable || sample.Volumes.Len() != 0 {
				t.Fatal("malformed volume damaged Node source or became zero usage")
			}
			if !strings.Contains(source.opts.Telemetry.Render(), "kubememlens_volume_stats_errors_total 1\n") {
				t.Fatal("volume failure was not observable")
			}
		})
	}
}

func TestMissingAndDisabledVolumeSources(t *testing.T) {
	for _, pods := range []string{`[]`, `[{` + podReference + `}]`, `[{` + podReference + `,"volume":null}]`, `[{` + podReference + `,"volume":[{"name":"missing"},{` + volumeValues + `}]}]`} {
		source, _ := volumeHarness(t, withVolumePods(pods), VolumeStatsEnabled)
		sample, err := source.ReadSample(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if sample.Volumes == nil || sample.Volumes.Len() != 0 || sample.Volumes.State().Availability != volumehealth.Reported {
			t.Fatal("absent optional stats became a failed or measured record")
		}
		if strings.Contains(pods, `"missing"`) && !strings.Contains(source.opts.Telemetry.Render(), "kubememlens_volume_stats_omissions_total 2\n") {
			t.Fatal("omissions not counted")
		}
	}
	source, _ := volumeHarness(t, withVolumePods(`[{"volume":[{"usedBytes":-1}]}]`), VolumeStatsDisabled)
	sample, err := source.ReadSample(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if sample.Volumes != nil || sample.Node.Stats == nil || strings.Contains(source.opts.Telemetry.Render(), "kubememlens_volume_stats_") {
		t.Fatal("disabled profile activated volume collection")
	}
}

func TestVolumeRecordOrderingAndBounds(t *testing.T) {
	volume := `{"name":"data",` + volumeValues + `}`
	uniqueVolumes := make([]string, volumecontext.MaxVolumesPerPod+1)
	for i := range uniqueVolumes {
		uniqueVolumes[i] = fmt.Sprintf(`{"name":"volume-%d"}`, i)
	}
	for name, pods := range map[string]string{
		"duplicate volume": `[{` + podReference + `,"volume":[` + volume + `,` + volume + `]}]`,
		"duplicate Pod":    `[{` + podReference + `,"volume":[` + volume + `]},{` + podReference + `,"volume":[` + volume + `]}]`,
		"too many volumes": `[{` + podReference + `,"volume":[` + strings.Join(uniqueVolumes, ",") + `]}]`,
		"too many Pods":    `[` + strings.Repeat(`{},`, volumecontext.MaxBatchRecords) + `{}]`,
	} {
		t.Run(name, func(t *testing.T) {
			source, _ := volumeHarness(t, withVolumePods(pods), VolumeStatsEnabled)
			sample, err := source.ReadSample(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if sample.Volumes == nil || sample.Volumes.State().Availability != volumehealth.Unavailable {
				t.Fatal("ambiguous or oversized source accepted")
			}
		})
	}
	rows, _, err := decodeVolumes(t.Context(), []byte(withVolumePods(`[{"volume":[`+volume+`],`+podReference+`}]`)), "node-uid-a")
	if err != nil || len(rows) != 1 || rows[0].PodUID != "pod-uid" {
		t.Fatal("field order changed identity binding")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := decodeVolumes(ctx, []byte(withVolumePods(`[]`)), "node-uid-a"); err == nil {
		t.Fatal("decoder ignored cancellation")
	}
}

func TestVolumeSnapshotCountIncludesOmittedRecords(t *testing.T) {
	volumes := make([]string, volumecontext.MaxVolumesPerPod)
	for i := range volumes {
		volumes[i] = fmt.Sprintf(`{"name":"volume-%d"}`, i)
	}
	pod := `{` + podReference + `,"volume":[` + strings.Join(volumes, ",") + `]}`
	for _, count := range []int{16, 17} {
		pods := `[` + strings.TrimSuffix(strings.Repeat(pod+",", count), ",") + `]`
		rows, omitted, err := decodeVolumes(t.Context(), []byte(withVolumePods(pods)), "node-uid-a")
		if count == 16 && (err != nil || len(rows) != 0 || omitted != volumecontext.MaxBatchRecords) {
			t.Fatal("valid omitted-record boundary failed", err)
		}
		if count == 17 && err == nil {
			t.Fatal("omitted records bypassed the total volume bound")
		}
	}
}

func FuzzVolumes(f *testing.F) {
	f.Add([]byte(withVolumePods(`[{` + podReference + `,"volume":[{"name":"data",` + volumeValues + `}]}]`)))
	f.Fuzz(func(t *testing.T, data []byte) {
		rows, omitted, err := decodeVolumes(t.Context(), data, "node-uid-a")
		if err != nil {
			return
		}
		if len(rows) > volumecontext.MaxBatchRecords || omitted < 0 {
			t.Fatal("unbounded volume decode")
		}
		batch, err := volumecontext.NewBatch("node-a", "node-uid-a", sampleNow, volumecontext.SourceState(volumehealth.Reported, ""), rows, sampleNow)
		if err != nil {
			return
		}
		encoded, err := batch.EncodePrivate()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := volumecontext.DecodePrivate(encoded, sampleNow.Add(time.Second)); err != nil {
			t.Fatal("source batch cannot use private transport")
		}
	})
}
