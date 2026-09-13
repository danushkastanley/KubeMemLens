package trace

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

func targetFixture() TargetIdentity {
	return TargetIdentity{
		Namespace: "tenant-a", PodName: "cache-reader", PodUID: "pod-uid-1",
		ContainerName: "reader", ContainerID: strings.Repeat("a", 64),
		ContainerStartedAt: time.Date(2026, 9, 13, 1, 2, 3, 0, time.UTC),
		NodeUID:            "node-uid-1", CgroupID: 42,
	}
}

func TestSpecificationCannotUseDiagnosticIdentityFallbacks(t *testing.T) {
	tests := map[string]func(*TargetIdentity){
		"short container ID": func(t *TargetIdentity) { t.ContainerID = t.ContainerID[:12] },
		"runtime prefix":     func(t *TargetIdentity) { t.ContainerID = "containerd://" + t.ContainerID },
		"nonhex ID":          func(t *TargetIdentity) { t.ContainerID = strings.Repeat("g", 64) },
		"no pod UID":         func(t *TargetIdentity) { t.PodUID = "" },
		"no node UID":        func(t *TargetIdentity) { t.NodeUID = "" },
		"no lifetime":        func(t *TargetIdentity) { t.ContainerStartedAt = time.Time{} },
		"no cgroup":          func(t *TargetIdentity) { t.CgroupID = 0 },
		"control character":  func(t *TargetIdentity) { t.Namespace = "tenant\nother" },
		"oversized UID":      func(t *TargetIdentity) { t.PodUID = strings.Repeat("a", 129) },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			target := targetFixture()
			mutate(&target)
			if _, err := NewSpecification(Files, target, OmitPaths, DefaultBounds()); err == nil {
				t.Fatal("invalid binding accepted")
			}
		})
	}
}

func TestSpecificationEnforcesAbsoluteCeilings(t *testing.T) {
	tests := map[string]func(*Bounds){
		"duration":          func(b *Bounds) { b.Duration = 5*time.Minute + time.Nanosecond },
		"negative duration": func(b *Bounds) { b.Duration = -time.Second },
		"zero events":       func(b *Bounds) { b.Events = 0 },
		"events":            func(b *Bounds) { b.Events = 100_001 },
		"bytes":             func(b *Bounds) { b.OutputBytes = (32 << 20) + 1 },
		"overflow bytes":    func(b *Bounds) { b.OutputBytes = math.MaxUint64 },
		"map bytes":         func(b *Bounds) { b.MapBytes = (32 << 20) + 1 },
		"paths":             func(b *Bounds) { b.PathBytes = 513 },
		"zero map":          func(b *Bounds) { b.MapBytes = 0 },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			bounds := DefaultBounds()
			mutate(&bounds)
			if _, err := NewSpecification(Files, targetFixture(), OmitPaths, bounds); err == nil {
				t.Fatal("invalid bounds accepted")
			}
		})
	}
	for _, bounds := range []Bounds{
		DefaultBounds(),
		{Duration: time.Second, Events: 1, OutputBytes: 1024, MapBytes: 4096, PathBytes: 16},
		{Duration: 5 * time.Minute, Events: 100_000, OutputBytes: 32 << 20, MapBytes: 32 << 20, PathBytes: 512},
	} {
		spec, err := NewSpecification(Files, targetFixture(), OmitPaths, bounds)
		if err != nil || spec.Bounds() != bounds {
			t.Fatalf("valid bounds changed or rejected: %v", err)
		}
	}
}

func TestSpecificationFixedVocabularyAndValueOwnership(t *testing.T) {
	for _, kind := range []Kind{"", "trace_exec", "ghcr.io/example/gadget:latest", "files --all-namespaces"} {
		if _, err := NewSpecification(kind, targetFixture(), OmitPaths, DefaultBounds()); err == nil {
			t.Fatal("arbitrary engine selection accepted")
		}
	}
	if err := (Specification{}).Validate(); err == nil {
		t.Fatal("zero specification accepted")
	}
	for _, kind := range []Kind{Files, Cache, OOM} {
		spec, err := NewSpecification(kind, targetFixture(), OmitPaths, DefaultBounds())
		if err != nil || spec.Validate() != nil || spec.Kind() != kind || spec.Paths() != OmitPaths {
			t.Fatalf("valid fixed kind rejected: %v", err)
		}
		copy := spec.Target()
		copy.CgroupID++
		if spec.Target().CgroupID != 42 {
			t.Fatal("target accessor changed the specification")
		}
	}
	if _, err := NewSpecification(OOM, targetFixture(), ConfirmedPaths, DefaultBounds()); err == nil {
		t.Fatal("raw-path mode accepted for an OOM trace")
	}
	if _, err := NewSpecification(Files, targetFixture(), "yes", DefaultBounds()); err == nil {
		t.Fatal("unrecognised consent state accepted")
	}
}

func TestInternalTraceValuesRejectAccidentalRetention(t *testing.T) {
	const secret = "/tenant-secret/token-file"
	path, err := NewSensitiveText(secret, 256)
	if err != nil || path.Reveal() != secret {
		t.Fatalf("explicit ephemeral access failed: %v", err)
	}
	spec, err := NewSpecification(Files, targetFixture(), ConfirmedPaths, DefaultBounds())
	if err != nil {
		t.Fatal(err)
	}
	pid := uint32(1234567)
	for _, value := range []any{
		path, targetFixture(), spec, FileActivity{Path: path},
		CacheActivity{Pages: 1}, OOMDecision{Command: path, VictimPID: &pid},
		struct{ Event OOMDecision }{OOMDecision{Command: path, VictimPID: &pid}},
	} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			encoded := fmt.Sprintf(format, value)
			for _, private := range []string{secret, "tenant-a", "pod-uid-1", "1234567"} {
				if strings.Contains(encoded, private) {
					t.Fatal("default formatting exposed trace identity")
				}
			}
		}
		if _, err := json.Marshal(value); err == nil {
			t.Fatal("implicit JSON retention accepted")
		}
	}
}

func TestSensitiveTextLimitsBytesWithoutTruncatingEvidence(t *testing.T) {
	for _, value := range []string{strings.Repeat("a", 513), strings.Repeat("é", 257)} {
		if _, err := NewSensitiveText(value, 512); err == nil {
			t.Fatal("oversized text accepted")
		}
	}
	if _, err := NewSensitiveText("short", math.MaxUint64); err == nil {
		t.Fatal("caller raised absolute text ceiling")
	}
	if _, err := NewSensitiveText(strings.Repeat("é", 256), 512); err != nil {
		t.Fatal(err)
	}
}
