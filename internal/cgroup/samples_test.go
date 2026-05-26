package cgroup

import (
	"path/filepath"
	"testing"
)

func TestParseSampleDirectories(t *testing.T) {
	root := filepath.Join("..", "..", "examples", "cgroup-v2")
	for _, sample := range []string{"cache-heavy", "rss-heavy", "tmpfs-heavy", "dirty-heavy", "normal"} {
		t.Run(sample, func(t *testing.T) {
			breakdown, err := ParseDirectory(sample, filepath.Join(root, sample))
			if err != nil {
				t.Fatalf("ParseDirectory returned error: %v", err)
			}
			if breakdown.TotalBytes == 0 {
				t.Fatal("TotalBytes = 0, want sample total")
			}
			if breakdown.AnonBytes == 0 {
				t.Fatal("AnonBytes = 0, want sample anon")
			}
		})
	}
}
