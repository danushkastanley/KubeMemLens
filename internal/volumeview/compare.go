package volumeview

import (
	"fmt"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/explain"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"github.com/danushkastanley/kube-memlens/internal/nodeview"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
)

func ComparisonLines(before, after explain.VolumeInput, result explain.VolumeComparison, width int) []string {
	lines := []string{"Volume and memory comparison", "Before: " + before.Pod.Namespace + "/" + before.Pod.PodName + " | " + instant(before.Now), "After: " + after.Pod.Namespace + "/" + after.Pod.PodName + " | " + instant(after.Now)}
	for _, row := range []struct {
		name string
		b, a uint64
	}{{"Memory charge", before.Pod.Memory.TotalBytes, after.Pod.Memory.TotalBytes}, {"File cache", before.Pod.Memory.CacheBytes(), after.Pod.Memory.CacheBytes()}, {"Shmem", before.Pod.Memory.ShmemBytes, after.Pod.Memory.ShmemBytes}, {"Dirty/writeback", before.Pod.Memory.DirtyWritebackBytes(), after.Pod.Memory.DirtyWritebackBytes()}} {
		text := row.name + ": " + model.FormatCompactBytes(row.b) + " -> " + model.FormatCompactBytes(row.a)
		if result.MemoryComparable {
			text += " | change " + signed(row.b, row.a)
		}
		lines = append(lines, text)
	}
	for _, row := range result.Rows {
		b, a := before.Volumes.Volumes[row.Before], after.Volumes.Volumes[row.After]
		lines = append(lines, "", "Volume binding: "+b.VolumeName+" -> "+a.VolumeName)
		if row.SharedClaim {
			lines = append(lines, "Shared PVC binding; these are not per-Pod usage measurements.")
		}
		lines = append(lines, comparisonVolumeLines("Before", b, before.Now)...)
		lines = append(lines, comparisonVolumeLines("After", a, after.Now)...)
		if row.UsageComparable {
			bf, af := b.Usage.Filesystem, a.Usage.Filesystem
			for _, field := range []struct {
				name string
				b, a *uint64
			}{{"Used bytes", bf.UsedBytes, af.UsedBytes}, {"Available bytes", bf.AvailableBytes, af.AvailableBytes}, {"Capacity bytes", bf.CapacityBytes, af.CapacityBytes}, {"Inodes used", bf.InodesUsed, af.InodesUsed}} {
				if field.b != nil && field.a != nil {
					delta := signed(*field.b, *field.a)
					if field.name == "Inodes used" {
						delta = signedCount(*field.b, *field.a)
					}
					lines = append(lines, field.name+" change: "+delta)
				}
			}
		} else {
			lines = append(lines, "Filesystem delta withheld: current, fresh and ordered source samples are required.")
		}
	}
	for _, entry := range []struct {
		label   string
		input   explain.VolumeInput
		indexes []int
	}{{"Before", before, result.UnmatchedBefore}, {"After", after, result.UnmatchedAfter}} {
		for _, index := range entry.indexes {
			v := entry.input.Volumes.Volumes[index]
			lines = append(lines, "", entry.label+" volume: "+v.VolumeName)
			lines = append(lines, comparisonVolumeLines(entry.label, v, entry.input.Now)...)
		}
	}
	for _, caveat := range result.Caveats {
		lines = append(lines, "- "+caveat)
	}
	for _, entry := range []struct {
		label string
		input explain.VolumeInput
	}{{"Before", before}, {"After", after}} {
		io := explain.AnalyzeVolumes(entry.input).IO
		line := fmt.Sprintf("%s I/O pressure: %d/%d containers available", entry.label, io.Available, io.Containers)
		if io.Available > 0 {
			line += fmt.Sprintf("; highest some/full avg10 %.2f%%/%.2f%%", io.SomeMax10, io.FullMax10)
		}
		lines = append(lines, line)
	}
	return nodeview.Wrap(lines, width)
}

func comparisonVolumeLines(label string, v volumecontext.NamedVolume, now time.Time) []string {
	lines := []string{fmt.Sprintf("%s usage: %s | %s | %s", label, v.Usage.Availability, v.Usage.Freshness, v.Usage.Reason)}
	lines = append(lines, filesystemLines(label+" filesystem", v.Usage.Filesystem, now)...)
	if v.Usage.LastGood != nil {
		lines = append(lines, filesystemLines(label+" historical filesystem", v.Usage.LastGood, now)...)
	}
	for _, h := range v.Health {
		lines = append(lines, label+" health:")
		lines = append(lines, healthLines(h.HealthReport, false, now)...)
		if h.LastGood != nil {
			lines = append(lines, healthLines(*h.LastGood, true, now)...)
		}
	}
	return lines
}
func signed(before, after uint64) string {
	if after >= before {
		return "+" + model.FormatCompactBytes(after-before)
	}
	return "-" + model.FormatCompactBytes(before-after)
}
func signedCount(before, after uint64) string {
	if after >= before {
		return fmt.Sprintf("+%d", after-before)
	}
	return fmt.Sprintf("-%d", before-after)
}
