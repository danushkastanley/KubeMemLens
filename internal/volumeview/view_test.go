package volumeview

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

func number(v uint64) *uint64 { return &v }

func viewFixture() (volumecontext.View, time.Time) {
	now := time.Now().UTC()
	h := volumehealth.Evaluate(volumehealth.Observation{Source: volumehealth.ControllerSource, Scope: volumehealth.VolumeScope, Availability: volumehealth.Reported, ObservedAt: now}, now)
	return volumecontext.View{SchemaVersion: 1, Namespace: "tenant", PodName: "app", Volumes: []volumecontext.NamedVolume{{VolumeName: "data", PVCName: "claim", Configuration: volumecontext.Configuration{Kind: volumecontext.PersistentClaim, MountCount: 2, ReadOnlyMountCount: 1},
		Usage: volumecontext.Usage{Source: "kubelet-summary", Availability: volumehealth.Reported, Freshness: volumehealth.Fresh, Completeness: capability.Partial,
			Filesystem: &volumecontext.Filesystem{CapturedAt: now, CapacityBytes: number(100), UsedBytes: number(0), AvailableBytes: number(90)}},
		Health: []volumecontext.Health{{HealthReport: volumecontext.HealthReport{Observation: h}}}}}}, now
}

func TestVolumeTextKeepsDomainsAndOptionality(t *testing.T) {
	view, now := viewFixture()
	result := explain.VolumeResult{MemorySeverity: explain.SeverityInfo, StorageSeverity: explain.SeverityHigh}
	for _, width := range []int{40, 76, 80, 116, 156} {
		lines := Lines(view, result, now, width)
		for _, line := range lines {
			if ansi.StringWidth(line) > width || strings.ContainsRune(line, '\x1b') {
				t.Fatalf("unbounded terminal line %q", line)
			}
		}
		text := strings.Join(lines, "\n")
		for _, phrase := range []string{"Volume: data", "PVC: claim", "Used: 0", "0.0%", "Inodes total: unreported", "Filesystem source:", "pvc-controller-plugin", "probe unknown"} {
			if !strings.Contains(strings.ReplaceAll(text, "\n", " "), phrase) {
				t.Fatalf("missing %q at width%d: %s", phrase, width, text)
			}
		}
	}
}

func TestVolumeMissingReasonAndHostileText(t *testing.T) {
	view, now := viewFixture()
	view.Volumes[0].Usage = volumecontext.SourceState(volumehealth.Forbidden, volumecontext.AccessDenied)
	text := strings.Join(Lines(view, explain.VolumeResult{}, now, 80), "\n")
	if !strings.Contains(text, "access-denied") || !strings.Contains(text, "Filesystem: unreported") {
		t.Fatal("missing evidence reason concealed")
	}
	view.Volumes[0].VolumeName = "\x1b[2Jsecret"
	text = strings.Join(Lines(view, explain.VolumeResult{}, now, 20), "\n")
	if strings.Contains(text, "secret") || strings.ContainsRune(text, '\x1b') {
		t.Fatal("hostile identity rendered")
	}
}

func TestFollowupCommandsAreReadOnlyAndNameBounded(t *testing.T) {
	view, _ := viewFixture()
	view.Volumes = append(view.Volumes, view.Volumes[0])
	commands := Commands(view)
	if len(commands) != 3 || commands[2] != "kubectl describe pvc 'claim' -n 'tenant'" {
		t.Fatalf("commands: %v", commands)
	}
	for _, name := range []string{"--context=other", "$(touch /tmp/bad)", "app\nother", "'app'", "-bad"} {
		view.PodName = name
		if Commands(view) != nil {
			t.Fatalf("invalid command target %q accepted", name)
		}
	}
	if Quote("a'b $(x)") != "'a'\"'\"'b $(x)'" {
		t.Fatal("shell quote convention changed")
	}
}
