// Package volumeview renders authorised evidence without joining or diagnosing it.
package volumeview

import (
	"fmt"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"github.com/danushkastanley/kube-memlens/internal/nodeview"
	"github.com/danushkastanley/kube-memlens/internal/recommend"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"k8s.io/apimachinery/pkg/util/validation"
)

func Lines(view volumecontext.View, result explain.VolumeResult, now time.Time, width int) []string {
	aged, err := volumecontext.AgeView(view, now)
	if err != nil {
		return nodeview.Wrap([]string{"Volume context is unavailable or expired; refresh the authorised read."}, width)
	}
	view = aged
	lines := []string{"Volumes: " + view.Namespace + "/" + view.PodName,
		"Memory severity: " + string(result.MemorySeverity) + " | Storage severity: " + string(result.StorageSeverity),
		"Filesystem bytes are separate from memory charge and composition."}
	if len(view.Volumes) == 0 {
		lines = append(lines, "No volumes in the authorised Pod configuration.")
	}
	for _, v := range view.Volumes {
		label := "Volume: " + v.VolumeName
		if v.PVCName != "" {
			label += " | PVC: " + v.PVCName
		}
		lines = append(lines, "", label, "Configuration source: Kubernetes Pod spec", "Kind: "+string(v.Configuration.Kind))
		if v.Driver != "" {
			lines = append(lines, "Authorised CSI driver: "+v.Driver)
		}
		if v.Configuration.MemoryBacked {
			lines = append(lines, "Memory-backed emptyDir: yes | Configured size limit: "+bytes(v.Configuration.SizeLimitBytes))
		}
		lines = append(lines, fmt.Sprintf("Mounts: %d (%d read-only); mount paths omitted", v.Configuration.MountCount, v.Configuration.ReadOnlyMountCount),
			fmt.Sprintf("Filesystem source: %s | %s | %s | %s", v.Usage.Source, v.Usage.Availability, v.Usage.Freshness, v.Usage.Completeness))
		if v.Usage.Reason != "" {
			lines = append(lines, "Filesystem reason: "+string(v.Usage.Reason))
		}
		lines = append(lines, filesystemLines("Filesystem", v.Usage.Filesystem, now)...)
		if v.Usage.LastGood != nil {
			lines = append(lines, filesystemLines("Historical filesystem (last good)", v.Usage.LastGood, now)...)
		}
		for _, h := range v.Health {
			lines = append(lines, healthLines(h.HealthReport, false, now)...)
			if h.LastGood != nil {
				lines = append(lines, healthLines(*h.LastGood, true, now)...)
			}
		}
	}
	io := result.IO
	lines = append(lines, "", "I/O source: "+model.IOPressureSource+" (container scope)",
		fmt.Sprintf("Coverage: %d/%d available; %d unreported, %d unavailable, %d invalid, %d stale", io.Available, io.Containers, io.Unreported, io.Unavailable, io.Invalid, io.Stale))
	if io.Available > 0 {
		lines = append(lines, "I/O samples: "+sample(io.OldestSample, now)+" to "+sample(io.NewestSample, now))
		lines = append(lines, fmt.Sprintf("Highest container some/full avg10: %.2f%% / %.2f%%; percentages are not summed", io.SomeMax10, io.FullMax10))
	}
	lines = append(lines, "", "Supporting evidence:")
	for _, s := range result.Signals {
		label := string(s.Kind) + " [" + s.Source + "]"
		if s.Historical {
			label += " historical"
		}
		lines = append(lines, label+": "+s.Summary)
	}
	lines = append(lines, "", "Uncertainty:")
	for _, caveat := range result.Caveats {
		lines = append(lines, "- "+caveat)
	}
	for _, item := range recommend.ForVolumes(result) {
		lines = append(lines, "", "Next check: "+item.Action, item.Rationale)
		for _, condition := range item.Conditions {
			lines = append(lines, "- "+condition)
		}
	}
	return nodeview.Wrap(lines, width)
}

func filesystemLines(label string, fs *volumecontext.Filesystem, now time.Time) []string {
	if fs == nil {
		return []string{label + ": unreported"}
	}
	return []string{label + " sampled: " + sample(fs.CapturedAt, now),
		"Capacity: " + bytes(fs.CapacityBytes) + " | Used: " + bytes(fs.UsedBytes) + " (" + percent(fs.UsedBytes, fs.CapacityBytes) + ")",
		"Available: " + bytes(fs.AvailableBytes) + " | reserved space may make used + available differ from capacity",
		"Inodes total: " + count(fs.Inodes) + " | used: " + count(fs.InodesUsed) + " (" + percent(fs.InodesUsed, fs.Inodes) + ") | free: " + count(fs.InodesFree)}
}

func healthLines(h volumecontext.HealthReport, historical bool, now time.Time) []string {
	o := h.Observation
	label := "Health"
	if historical {
		label = "Historical health (last good)"
	}
	lines := []string{fmt.Sprintf("%s: %s | scope %s | %s", label, o.Source, o.Scope, o.State),
		fmt.Sprintf("Availability: %s | observation %s | probe %s", o.Availability, o.ObservationFreshness, o.ProbeFreshness),
		"API observed: " + sample(o.ObservedAt, now)}
	if o.Reason != "" {
		lines = append(lines, "Health reason: "+string(o.Reason))
	}
	for _, c := range h.Conditions {
		lines = append(lines, "Condition: "+string(c.Status)+" | transition: "+instant(c.TransitionAt))
	}
	return lines
}

func bytes(v *uint64) string {
	if v == nil {
		return "unreported"
	}
	return model.FormatCompactBytes(*v)
}
func count(v *uint64) string {
	if v == nil {
		return "unreported"
	}
	return fmt.Sprint(*v)
}
func percent(part, total *uint64) string {
	if part == nil || total == nil || *total == 0 {
		return "unreported"
	}
	return fmt.Sprintf("%.1f%%", float64(*part)/float64(*total)*100)
}
func instant(t time.Time) string {
	if t.IsZero() {
		return "unreported"
	}
	return t.UTC().Format(time.RFC3339)
}
func sample(at, now time.Time) string {
	if at.IsZero() {
		return "unreported"
	}
	return fmt.Sprintf("%s (age %s)", instant(at), max(time.Duration(0), now.Sub(at)).Round(time.Second))
}

// Quote follows the existing CLI clipboard single-quote convention. Quotes
// protect every argument, including kubeconfig paths supplied by the operator.
func Quote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

func Commands(view volumecontext.View) []string {
	// Validate names separately from time-dependent measurements: follow-up
	// commands remain useful when those measurements have aged.
	if !validCommandName(view.Namespace) || !validCommandName(view.PodName) {
		return nil
	}
	commands := []string{"kubectl get pod " + Quote(view.PodName) + " -n " + Quote(view.Namespace),
		"kubectl memlens history pod " + Quote(view.PodName) + " -n " + Quote(view.Namespace)}
	seen := map[string]bool{}
	for _, v := range view.Volumes {
		if validCommandName(v.PVCName) && !seen[v.PVCName] {
			commands = append(commands, "kubectl describe pvc "+Quote(v.PVCName)+" -n "+Quote(view.Namespace))
			seen[v.PVCName] = true
		}
	}
	return commands
}

func validCommandName(value string) bool {
	return len(validation.IsDNS1123Subdomain(value)) == 0
}
