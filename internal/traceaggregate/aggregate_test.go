package traceaggregate

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func number(value uint64) *uint64 { return &value }

func TestFileTotalsKeepRequestedAndCompletedSeparate(t *testing.T) {
	a, err := New(trace.Files, 10)
	if err != nil {
		t.Fatal(err)
	}
	path, _ := trace.NewSensitiveText("/private-marker", 64)
	for _, event := range []trace.FileActivity{
		{Operation: trace.FileRead, RequestedBytes: number(4096), CompletedBytes: number(512), Path: path},
		{Operation: trace.FileRead, RequestedBytes: number(1024), CompletedBytes: number(0)},
		{Operation: trace.FileWrite, RequestedBytes: number(2048), CompletedBytes: number(1024)},
	} {
		if a.File(event) != nil {
			t.Fatal("valid observation rejected")
		}
	}
	summary := a.Snapshot()
	if summary.Observations != 3 || summary.Reads.Operations != 2 || *summary.Reads.RequestedBytes.Value != 5120 || *summary.Reads.CompletedBytes.Value != 512 || summary.Writes.Operations != 1 || *summary.Writes.CompletedBytes.Value != 1024 {
		t.Fatal("file semantics changed during aggregation")
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", summary, summary), "private-marker") {
		t.Fatal("aggregate retained path")
	}
	if _, err := json.Marshal(summary); err == nil {
		t.Fatal("aggregate bypassed ephemeral transport")
	}
	*summary.Reads.RequestedBytes.Value = 0
	if *a.Snapshot().Reads.RequestedBytes.Value != 5120 {
		t.Fatal("snapshot mutated accumulator")
	}
}

func TestMissingAndOverflowAreNotReportedAsZero(t *testing.T) {
	a, _ := New(trace.Files, 10)
	for _, event := range []trace.FileActivity{
		{Operation: trace.FileRead, RequestedBytes: nil, CompletedBytes: number(0)},
		{Operation: trace.FileRead, RequestedBytes: number(math.MaxUint64), CompletedBytes: number(0)},
		{Operation: trace.FileRead, RequestedBytes: number(1), CompletedBytes: number(0)},
	} {
		if a.File(event) != nil {
			t.Fatal("valid observation rejected")
		}
	}
	s := a.Snapshot()
	if s.Reads.RequestedBytes.Value != nil || !s.Reads.RequestedBytes.Unreported || !s.Reads.RequestedBytes.Overflow {
		t.Fatal("missing/overflow evidence lost")
	}
	if s.Reads.CompletedBytes.Value == nil || *s.Reads.CompletedBytes.Value != 0 || s.Reads.CompletedBytes.Unreported {
		t.Fatal("measured zero confused with missing")
	}
	if s.Writes.RequestedBytes.Value == nil || *s.Writes.RequestedBytes.Value != 0 || s.Writes.Operations != 0 {
		t.Fatal("empty observed set did not sum to zero")
	}
}

func TestCachePagesAndOperationCountsRemainDistinct(t *testing.T) {
	a, _ := New(trace.Cache, 10)
	for _, event := range []trace.CacheActivity{{Operation: trace.CacheAdd, Pages: 512}, {Operation: trace.CacheAdd, Pages: 1}, {Operation: trace.CacheRemove, Pages: 8}} {
		if a.Cache(event) != nil {
			t.Fatal("valid cache observation rejected")
		}
	}
	s := a.Snapshot()
	if s.Observations != 3 || s.Additions.Operations != 2 || *s.Additions.Pages.Value != 513 || s.Removals.Operations != 1 || *s.Removals.Pages.Value != 8 {
		t.Fatal("cache operations were confused with page counts")
	}
}

func TestInvalidObservationsDoNotConsumeBudget(t *testing.T) {
	a, _ := New(trace.Files, 1)
	if a.File(trace.FileActivity{Operation: trace.FileOpen}) != ErrObservation || a.File(trace.FileActivity{Operation: trace.FileRead, RequestedBytes: number(1), CompletedBytes: number(2)}) != ErrObservation || a.Cache(trace.CacheActivity{Operation: trace.CacheAdd, Pages: 1}) != ErrObservation {
		t.Fatal("invalid observation accepted")
	}
	if a.Snapshot().Observations != 0 {
		t.Fatal("invalid observation changed aggregates")
	}
	if a.File(trace.FileActivity{Operation: trace.FileRead}) != nil {
		t.Fatal("invalid input consumed budget")
	}
	for range 2 {
		if a.File(trace.FileActivity{Operation: trace.FileRead}) != ErrLimit {
			t.Fatal("observation ceiling not retained")
		}
	}
	if a.Snapshot().Observations != 1 {
		t.Fatal("limit changed aggregate")
	}
}

func TestInputPointersAreCopied(t *testing.T) {
	a, _ := New(trace.Files, 1)
	value := uint64(10)
	if a.File(trace.FileActivity{Operation: trace.FileRead, RequestedBytes: &value, CompletedBytes: &value}) != nil {
		t.Fatal("valid event rejected")
	}
	value = 999
	if *a.Snapshot().Reads.CompletedBytes.Value != 10 {
		t.Fatal("input mutation changed totals")
	}
}

func TestCacheOverflowAndInvalidRecords(t *testing.T) {
	a, _ := New(trace.Cache, 4)
	if a.Cache(trace.CacheActivity{Operation: trace.CacheAdd}) != ErrObservation || a.Cache(trace.CacheActivity{Operation: "hit", Pages: 1}) != ErrObservation {
		t.Fatal("invalid cache semantics accepted")
	}
	for range 4 {
		if a.Cache(trace.CacheActivity{Operation: trace.CacheAdd, Pages: 1 << 62}) != nil {
			t.Fatal("valid cache observation rejected")
		}
	}
	if a.Cache(trace.CacheActivity{Operation: trace.CacheRemove, Pages: 1}) != ErrLimit {
		t.Fatal("cache ceiling not enforced")
	}
	s := a.Snapshot()
	if s.Additions.Pages.Value != nil || !s.Additions.Pages.Overflow || s.Additions.Pages.Unreported || s.Additions.Operations != 4 {
		t.Fatal("cache overflow was reported as a numeric total")
	}
}

func TestConstructorEnforcesFixedKindAndCeiling(t *testing.T) {
	for _, limit := range []uint64{0, 100001} {
		if _, err := New(trace.Files, limit); err == nil {
			t.Fatal("invalid ceiling accepted")
		}
	}
	if _, err := New(trace.OOM, 1); err == nil {
		t.Fatal("unsupported aggregate kind accepted")
	}
}
