package model

import "fmt"

const (
	kib uint64 = 1024
	mib        = kib * 1024
	gib        = mib * 1024
	tib        = gib * 1024
)

func FormatBytes(bytes uint64) string {
	switch {
	case bytes >= tib:
		return fmt.Sprintf("%.2f TiB", float64(bytes)/float64(tib))
	case bytes >= gib:
		return fmt.Sprintf("%.2f GiB", float64(bytes)/float64(gib))
	case bytes >= mib:
		return fmt.Sprintf("%.2f MiB", float64(bytes)/float64(mib))
	case bytes >= kib:
		return fmt.Sprintf("%.2f KiB", float64(bytes)/float64(kib))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func FormatCompactBytes(bytes uint64) string {
	switch {
	case bytes >= tib:
		return fmt.Sprintf("%.2fTi", float64(bytes)/float64(tib))
	case bytes >= gib:
		return fmt.Sprintf("%.2fGi", float64(bytes)/float64(gib))
	case bytes >= mib:
		value := float64(bytes) / float64(mib)
		if bytes%mib == 0 {
			return fmt.Sprintf("%.0fMi", value)
		}
		return fmt.Sprintf("%.2fMi", value)
	case bytes >= kib:
		value := float64(bytes) / float64(kib)
		if bytes%kib == 0 {
			return fmt.Sprintf("%.0fKi", value)
		}
		return fmt.Sprintf("%.2fKi", value)
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}

func FormatDirtyWriteback(m MemoryBreakdown) string {
	if m.DirtyWritebackBytes() == 0 {
		return "low"
	}

	if m.TotalBytes > 0 && m.DirtyWritebackRatio() < 0.01 {
		return "low"
	}

	return FormatBytes(m.DirtyWritebackBytes())
}
