package traceframe

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

// This is the 128-event cached-read burst used by the local qualification
// workload. It measures the production decoder without a kernel load, network
// transport or an implication that end-to-end resource limits have passed.
func benchmarkConfirmedFrames(b *testing.B) ([]byte, []byte) {
	b.Helper()
	started := time.Unix(200, 0).UTC()
	target := trace.TargetIdentity{Namespace: "fixture", PodName: "target", PodUID: "fixture-uid",
		ContainerName: "worker", ContainerID: strings.Repeat("a", 64),
		ContainerStartedAt: time.Unix(100, 0).UTC(), NodeUID: "fixture-node", CgroupID: 123}
	bounds := trace.DefaultBounds()
	bounds.Duration, bounds.PathBytes = 30*time.Second, 64
	specification, err := trace.NewSpecification(trace.Files, target, trace.ConfirmedPaths, bounds)
	if err != nil {
		b.Fatal(err)
	}
	metadata, err := NewMetadataVersion(Metadata{SessionID: strings.Repeat("b", 32),
		EngineDigest: "sha256:" + strings.Repeat("c", 64), ProgrammeDigest: "sha256:" + strings.Repeat("d", 64),
		Specification: specification, SessionStartedAt: started, Deadline: started.Add(bounds.Duration)}, AggregateVersion)
	if err != nil {
		b.Fatal(err)
	}
	path, err := trace.NewSensitiveText("/work/fixed-seed.bin", bounds.PathBytes)
	if err != nil {
		b.Fatal(err)
	}
	requested, completed := uint64(65536), uint64(65536)
	event, err := NewFileVersion(trace.FileActivity{ObservedAt: started.Add(time.Second), Operation: trace.FileRead,
		RequestedBytes: &requested, CompletedBytes: &completed, Path: path}, specification, AggregateVersion)
	if err != nil {
		b.Fatal(err)
	}
	header, err := Encode(metadata)
	if err != nil {
		b.Fatal(err)
	}
	record, err := Encode(event)
	if err != nil {
		b.Fatal(err)
	}
	burst := append(header, bytes.Repeat(record, 128)...)
	return burst, record
}

func BenchmarkReadConfirmedFileBurst(b *testing.B) {
	burst, _ := benchmarkConfirmedFrames(b)
	b.ReportAllocs()
	b.SetBytes(int64(len(burst)))
	b.ResetTimer()
	for range b.N {
		reader := NewReader(bytes.NewReader(burst))
		for range 129 {
			frame, err := reader.Next()
			if err != nil {
				b.Fatal(err)
			}
			if _, err := Encode(frame); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkDecodeConfirmedFileEvent(b *testing.B) {
	_, record := benchmarkConfirmedFrames(b)
	b.ReportAllocs()
	b.SetBytes(int64(len(record)))
	b.ResetTimer()
	for range b.N {
		if _, err := Decode(record); err != nil {
			b.Fatal(err)
		}
	}
}
