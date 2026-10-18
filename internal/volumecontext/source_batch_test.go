package volumecontext

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

func TestSourceBatchOmitsExpiredMeasurementsWithoutChangingCurrentValues(t *testing.T) {
	scope, _, rows := fixture()
	expired := rows[0]
	expired.PodUID = "expired-private-pod"
	expired.Filesystem.CapturedAt = testNow.Add(-ExpireAfter - time.Second)
	input := []RawUsage{expired, rows[0]}
	batch, omitted, err := NewSourceBatch(scope.NodeName, scope.NodeUID, testNow, SourceState(volumehealth.Reported, ""), input, testNow)
	if err != nil || omitted != 1 || batch.Len() != 1 || cap(batch.records) != batch.Len() {
		t.Fatal("expired source measurement retained or current record lost", omitted, err)
	}
	got := batch.ForPod(scope)
	if len(got) != 1 || !got[0].Filesystem.CapturedAt.Equal(testNow) || got[0].Filesystem.UsedBytes == nil || *got[0].Filesystem.UsedBytes != 0 || got[0].Filesystem.InodesFree != nil {
		t.Fatal("source clock, measured zero or missing field changed")
	}
	body, err := batch.EncodePrivate()
	if err != nil || strings.Contains(string(body), expired.PodUID) {
		t.Fatal("expired private record survived acquisition")
	}
	if _, err := DecodePrivate(body, testNow); err != nil {
		t.Fatal("filtered source cannot cross unchanged ingestion validation", err)
	}
	if len(input) != 2 || input[0].PodUID != expired.PodUID || !input[0].Filesystem.CapturedAt.Equal(expired.Filesystem.CapturedAt) {
		t.Fatal("acquisition mutated caller-owned input")
	}
}

func TestSourceBatchExpiryBoundaryAndEntirelyExpiredSource(t *testing.T) {
	scope, bindings, rows := fixture()
	for _, tc := range []struct {
		age     time.Duration
		omitted int
	}{{StaleAfter + time.Second, 0}, {ExpireAfter, 0}, {ExpireAfter + time.Nanosecond, 1}} {
		rows[0].Filesystem.CapturedAt = testNow.Add(-tc.age)
		batch, omitted, err := NewSourceBatch(scope.NodeName, scope.NodeUID, testNow, SourceState(volumehealth.Reported, ""), rows, testNow)
		if err != nil || omitted != tc.omitted || batch.Len() != 1-tc.omitted || batch.State().Availability != volumehealth.Reported {
			t.Fatal("expiry boundary changed", tc.age, omitted, err)
		}
		if omitted == 0 {
			retained := batch.ForPod(scope)[0].Filesystem
			if !retained.CapturedAt.Equal(rows[0].Filesystem.CapturedAt) || evaluateUsage(retained, testNow).Freshness != volumehealth.Stale {
				t.Fatal("unexpired stale source was retimestamped or labelled fresh")
			}
			continue
		}
		report, err := Join(scope, bindings, batch.ForPod(scope), nil, batch.State(), testNow)
		if err != nil {
			t.Fatal(err)
		}
		usage := report.Authorised().Volumes[0].Usage
		if usage.Availability != volumehealth.Unreported || usage.Filesystem != nil || usage.LastGood != nil {
			t.Fatal("entirely expired source became a measured or healthy value")
		}
	}
}

func TestSourceOmissionCannotHideMalformedExpiredRecords(t *testing.T) {
	for name, corrupt := range map[string]func(*RawUsage){
		"namespace":       func(r *RawUsage) { r.Namespace = "../invalid" },
		"Pod identity":    func(r *RawUsage) { r.PodUID = "invalid uid" },
		"Node identity":   func(r *RawUsage) { r.NodeUID = "another-node" },
		"claim scope":     func(r *RawUsage) { r.PVCNamespace = "another-tenant" },
		"volume name":     func(r *RawUsage) { r.VolumeName = "../invalid" },
		"missing time":    func(r *RawUsage) { r.Filesystem.CapturedAt = time.Time{} },
		"future time":     func(r *RawUsage) { r.Filesystem.CapturedAt = testNow.Add(FutureSkew + time.Second) },
		"missing metrics": func(r *RawUsage) { r.Filesystem = Filesystem{CapturedAt: r.Filesystem.CapturedAt} },
	} {
		t.Run(name, func(t *testing.T) {
			scope, _, rows := fixture()
			rows[0].Filesystem.CapturedAt = testNow.Add(-ExpireAfter - time.Second)
			corrupt(&rows[0])
			if _, _, err := NewSourceBatch(scope.NodeName, scope.NodeUID, testNow, SourceState(volumehealth.Reported, ""), rows, testNow); err == nil {
				t.Fatal("invalid source record was hidden by omission")
			}
		})
	}
}

func TestSourceOmissionCannotBypassDuplicateOrCountBounds(t *testing.T) {
	scope, _, rows := fixture()
	rows[0].Filesystem.CapturedAt = testNow.Add(-ExpireAfter - time.Second)
	for _, count := range []int{MaxVolumesPerPod + 1, MaxBatchRecords + 1} {
		input := make([]RawUsage, count)
		for i := range input {
			input[i] = rows[0]
			input[i].VolumeName = fmt.Sprintf("volume-%d", i%MaxVolumesPerPod)
			if count > MaxBatchRecords {
				input[i].PodUID = fmt.Sprintf("pod-%d", i/MaxVolumesPerPod)
			} else {
				input[i].VolumeName = fmt.Sprintf("volume-%d", i)
			}
		}
		if _, _, err := NewSourceBatch(scope.NodeName, scope.NodeUID, testNow, SourceState(volumehealth.Reported, ""), input, testNow); err == nil {
			t.Fatal("expired records bypassed a cardinality limit", count)
		}
	}
	if _, _, err := NewSourceBatch(scope.NodeName, scope.NodeUID, testNow, SourceState(volumehealth.Reported, ""), []RawUsage{rows[0], rows[0]}, testNow); err == nil {
		t.Fatal("expired duplicate accepted")
	}
}

func TestIngestionStillRejectsExpiredRowsInCurrentEnvelope(t *testing.T) {
	scope, _, rows := fixture()
	batch, err := NewBatch(scope.NodeName, scope.NodeUID, testNow, SourceState(volumehealth.Reported, ""), rows, testNow)
	if err != nil {
		t.Fatal(err)
	}
	rows[0].Filesystem.CapturedAt = testNow.Add(-ExpireAfter - time.Second)
	if _, err := NewBatch(scope.NodeName, scope.NodeUID, testNow, SourceState(volumehealth.Reported, ""), rows, testNow); err == nil {
		t.Fatal("strict batch constructor accepted expired data")
	}
	body, err := batch.EncodePrivate()
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatal(err)
	}
	record := wire["records"].([]any)[0].(map[string]any)
	record["filesystem"].(map[string]any)["capturedAt"] = rows[0].Filesystem.CapturedAt.Format(time.RFC3339Nano)
	body, err = json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodePrivate(body, testNow); err == nil {
		t.Fatal("wire ingestion accepted an expired row with a current envelope")
	}
}
