package filecache

import (
	"encoding/binary"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

func decoder(t testing.TB, kind trace.Kind, paths trace.PathPolicy) *Decoder {
	t.Helper()
	target := trace.TargetIdentity{Namespace: "fixture", PodName: "target", PodUID: "uid", ContainerName: "worker", ContainerID: strings.Repeat("a", 64), ContainerStartedAt: time.Unix(1, 0), NodeUID: "node-uid", CgroupID: 123}
	spec, err := trace.NewSpecification(kind, target, paths, trace.DefaultBounds())
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewDecoder(spec, Alignment{MonotonicNS: 100, WallTime: time.Unix(20, 0), Duration: time.Second, Uncertainty: time.Microsecond})
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func fileRecord(path string) []byte {
	data := make([]byte, 552)
	binary.LittleEndian.PutUint64(data[:8], 200)
	binary.LittleEndian.PutUint64(data[8:16], 100)
	binary.LittleEndian.PutUint64(data[16:24], 80)
	binary.LittleEndian.PutUint32(data[24:28], 1)
	binary.LittleEndian.PutUint32(data[28:32], uint32(len(path)))
	copy(data[32:], path)
	return data
}
func TestFileSemanticsAndPathBoundary(t *testing.T) {
	d := decoder(t, trace.Files, trace.OmitPaths)
	event, err := d.File(fileRecord(""))
	if err != nil || event.Operation != trace.FileRead || *event.RequestedBytes != 100 || *event.CompletedBytes != 80 || !event.ObservedAt.Equal(time.Unix(20, 100)) {
		t.Fatal("file operation or timestamp semantics changed")
	}
	if _, err := d.File(fileRecord("/private")); err == nil {
		t.Fatal("default policy accepted path bytes")
	}
	d = decoder(t, trace.Files, trace.ConfirmedPaths)
	event, err = d.File(fileRecord("/fixture/\x1bname"))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := traceframe.NewFile(event, d.spec)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := traceframe.Encode(frame)
	if err != nil || !strings.Contains(string(encoded), `\\u{001b}`) {
		t.Fatal("raw path bypassed the terminal-safe stream codec")
	}
}
func TestMalformedFileRecords(t *testing.T) {
	d := decoder(t, trace.Files, trace.ConfirmedPaths)
	cases := map[string]func([]byte){
		"hidden trailing path": func(b []byte) { b[100] = 'x' },
		"nonzero padding":      func(b []byte) { b[551] = 1 },
		"invalid utf8":         func(b []byte) { b[32] = 255 },
		"embedded nul":         func(b []byte) { b[32] = 0 },
		"completed overflow":   func(b []byte) { binary.LittleEndian.PutUint64(b[16:24], 101) },
		"unknown operation":    func(b []byte) { binary.LittleEndian.PutUint32(b[24:28], 3) },
		"oversized path":       func(b []byte) { binary.LittleEndian.PutUint32(b[28:32], 513) },
		"before observation":   func(b []byte) { binary.LittleEndian.PutUint64(b[:8], 99) },
		"after observation":    func(b []byte) { binary.LittleEndian.PutUint64(b[:8], 100+uint64(2*time.Second)) },
		"timestamp overflow":   func(b []byte) { binary.LittleEndian.PutUint64(b[:8], math.MaxUint64) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			data := fileRecord("/fixture")
			mutate(data)
			if _, err := d.File(data); err == nil {
				t.Fatal("malformed record accepted")
			}
		})
	}
	if _, err := d.File(fileRecord("")); err == nil {
		t.Fatal("confirmed record accepted a missing path")
	}
}
func TestCacheAndCounterSemantics(t *testing.T) {
	d := decoder(t, trace.Cache, trace.OmitPaths)
	data := make([]byte, 24)
	binary.LittleEndian.PutUint64(data[:8], 200)
	binary.LittleEndian.PutUint64(data[8:16], 512)
	binary.LittleEndian.PutUint32(data[16:20], 2)
	event, err := d.Cache(data)
	if err != nil || event.Operation != trace.CacheRemove || event.Pages != 512 {
		t.Fatal("folio order did not preserve base-page semantics")
	}
	binary.LittleEndian.PutUint64(data[8:16], 3)
	if _, err := d.Cache(data); err == nil {
		t.Fatal("non-folio page count accepted")
	}
	counters := make([]byte, 32)
	binary.LittleEndian.PutUint64(counters[:8], 10)
	binary.LittleEndian.PutUint64(counters[8:16], 2)
	binary.LittleEndian.PutUint64(counters[16:24], 3)
	binary.LittleEndian.PutUint64(counters[24:32], 1)
	counts, err := DecodeCounts(counters)
	if err != nil || *counts.Produced != 10 || *counts.Sampled != 2 || *counts.Lost != 3 || *counts.Rejected != 1 {
		t.Fatal("loss counts were hidden")
	}
	binary.LittleEndian.PutUint64(counters[16:24], math.MaxUint64)
	counts, err = DecodeCounts(counters)
	if err == nil || counts.Produced != nil || counts.Lost != nil {
		t.Fatal("counter overflow became known zero or a successful count")
	}
}
func FuzzFileRecord(f *testing.F) {
	d := decoder(f, trace.Files, trace.ConfirmedPaths)
	f.Add(fileRecord("/fixture"))
	f.Fuzz(func(t *testing.T, data []byte) {
		event, err := d.File(data)
		if err != nil {
			return
		}
		if _, err := traceframe.NewFile(event, d.spec); err != nil {
			t.Fatal("accepted record cannot enter the bounded stream")
		}
	})
}
