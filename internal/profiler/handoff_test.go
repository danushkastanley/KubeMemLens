package profiler

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

var observed = time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)

func fixture() api.PodSnapshot {
	c := api.ContainerSnapshot{Namespace: "test", PodName: "app-1", PodUID: "uid", ContainerName: "app", ContainerID: "container-id",
		CapturedAt: observed, Freshness: api.EvidenceFreshnessFresh, Completeness: api.EvidenceComplete,
		Context: api.ContainerContext{PodPhase: "Running", Labels: map[string]string{LabelPrefix + "app": GoHeapProfile}},
		Memory:  model.MemoryBreakdown{TotalBytes: 100 << 20, AnonBytes: 80 << 20},
	}
	return api.PodSnapshot{Namespace: c.Namespace, PodName: c.PodName, PodUID: c.PodUID, CapturedAt: observed,
		Freshness: c.Freshness, Completeness: c.Completeness, Context: api.PodContext{Phase: "Running"}, Containers: []api.ContainerSnapshot{c}}
}

func TestDeclarationAndEvidenceBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*api.PodSnapshot)
	}{
		{"absent", func(p *api.PodSnapshot) { p.Containers[0].Context.Labels = nil }},
		{"unsupported", func(p *api.PodSnapshot) { p.Containers[0].Context.Labels[LabelPrefix+"app"] = "java" }},
		{"wrong sidecar", func(p *api.PodSnapshot) {
			p.Containers[0].Context.Labels = map[string]string{LabelPrefix + "sidecar": GoHeapProfile}
		}},
		{"generic inference", func(p *api.PodSnapshot) {
			p.Containers[0].Context.Labels = map[string]string{"app": "golang", "runtime": "go"}
		}},
		{"stale Pod", func(p *api.PodSnapshot) { p.Freshness = api.EvidenceFreshnessStale }},
		{"partial container", func(p *api.PodSnapshot) { p.Containers[0].Completeness = api.EvidencePartial }},
		{"unknown completeness", func(p *api.PodSnapshot) { p.Completeness = "" }},
		{"old clock", func(p *api.PodSnapshot) { p.Containers[0].CapturedAt = observed.Add(-31 * time.Second) }},
		{"future clock", func(p *api.PodSnapshot) { p.CapturedAt = observed.Add(6 * time.Second) }},
		{"missing clock", func(p *api.PodSnapshot) { p.CapturedAt = time.Time{} }},
		{"terminated Pod", func(p *api.PodSnapshot) { p.Context.Phase = "Succeeded" }},
		{"wrong UID", func(p *api.PodSnapshot) { p.Containers[0].PodUID = "other" }},
		{"missing identity", func(p *api.PodSnapshot) { p.Containers[0].ContainerID = "" }},
		{"cache dominant", func(p *api.PodSnapshot) {
			p.Containers[0].Memory.AnonBytes = 1 << 20
			p.Containers[0].Memory.FileBytes = 80 << 20
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := fixture()
			tc.change(&p)
			got := ForPods([]api.PodSnapshot{p}, observed)
			if len(got) != 1 || got[0].Handoff != nil || got[0].State != "unavailable" || got[0].Reason == "" {
				t.Fatalf("unexpected guidance: %+v", got)
			}
		})
	}
	for _, delta := range []time.Duration{-30 * time.Second, 5 * time.Second} {
		p := fixture()
		p.CapturedAt = observed.Add(delta)
		p.Containers[0].CapturedAt = p.CapturedAt
		if ForPods([]api.PodSnapshot{p}, observed)[0].Handoff == nil {
			t.Fatalf("valid boundary %v rejected", delta)
		}
	}
}

func TestGoHandoffGolden(t *testing.T) {
	got := ForPods([]api.PodSnapshot{fixture()}, observed)
	if got[0].Handoff == nil {
		t.Fatalf("missing Go handoff: %+v", got)
	}
	body, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, '\n')
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile("testdata/go-heap.json", body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile("testdata/go-heap.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(want) {
		t.Fatalf("reviewed guidance changed:\n%s", body)
	}
	lines := strings.Join(Lines(got), "\n")
	for _, command := range got[0].Handoff.Commands {
		if !strings.Contains(lines, command) {
			t.Fatal("renderer lost command")
		}
	}
}

func TestDeterminismBoundsAndNoMutation(t *testing.T) {
	a, b := fixture(), fixture()
	b.PodName = "app-2"
	b.Containers[0].PodName = b.PodName
	input := []api.PodSnapshot{b, a}
	before, _ := json.Marshal(input)
	forward := ForPods(input, observed)
	reverse := ForPods([]api.PodSnapshot{a, b}, observed)
	after, _ := json.Marshal(input)
	if !reflect.DeepEqual(forward, reverse) || string(before) != string(after) {
		t.Fatal("order-dependent or mutated input")
	}
	forward[0].Handoff.Commands[0] = "changed"
	if reflect.DeepEqual(forward, ForPods(input, observed)) {
		t.Fatal("guidance shares mutable state")
	}
	for _, pods := range [][]api.PodSnapshot{nil, {a, a}, make([]api.PodSnapshot, MaxContainers+1)} {
		r := ForPods(pods, observed)
		if len(r) != 1 || r[0].Handoff != nil {
			t.Fatal("unbounded/ambiguous input accepted")
		}
	}
	a.Containers = make([]api.ContainerSnapshot, MaxContainers+1)
	if r := ForPods([]api.PodSnapshot{a}, observed); len(r) != 1 || r[0].Handoff != nil {
		t.Fatal("container bound ignored")
	}
}

func TestUntrustedMetadataNeverEntersGuidance(t *testing.T) {
	for _, value := range []string{"https://user:secret@example.test/profile", "$(touch /tmp/unwanted)", "go-heap-pprof-v1\n", "\x1b]52;c;secret\a"} {
		p := fixture()
		p.Containers[0].Context.Labels[LabelPrefix+"app"] = value
		r := ForPods([]api.PodSnapshot{p}, observed)
		body, _ := json.Marshal(r)
		if r[0].Handoff != nil || strings.Contains(string(body), "secret") || strings.Contains(string(body), "touch") {
			t.Fatal("untrusted declaration escaped")
		}
		p = fixture()
		p.PodName = value
		p.Containers[0].PodName = value
		r = ForPods([]api.PodSnapshot{p}, observed)
		if r[0].Pod != "" || r[0].Handoff != nil {
			t.Fatal("invalid display identity escaped")
		}
	}
}
