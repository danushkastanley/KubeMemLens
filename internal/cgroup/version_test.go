package cgroup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectVersionFromTheMountedRoot(t *testing.T) {
	tests := map[string]struct {
		files []string
		dirs  []string
		want  string
	}{
		"unified v2 root": {
			files: []string{"cgroup.controllers", "cgroup.subtree_control"},
			dirs:  []string{"kubepods.slice"},
			want:  VersionV2,
		},
		"v1 root": {
			dirs: []string{"memory/kubepods", "cpu,cpuacct", "pids"},
			want: VersionV1,
		},
		"hybrid root with the memory controller on v1": {
			files: []string{"unified/cgroup.controllers"},
			dirs:  []string{"memory", "cpu,cpuacct", "unified"},
			want:  VersionV1,
		},
		"empty root": {
			want: VersionUnknown,
		},
		"root with names of the wrong kind": {
			files: []string{"memory"},
			dirs:  []string{"cgroup.controllers"},
			want:  VersionUnknown,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			for _, dir := range test.dirs {
				if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			for _, file := range test.files {
				if err := os.WriteFile(filepath.Join(root, file), nil, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got, err := DetectVersion(root)
			if err != nil || got != test.want {
				t.Fatalf("DetectVersion(%s) = %q, %v; want %q, nil", name, got, err, test.want)
			}
		})
	}
}

// A marker that exists but cannot be read is not evidence of either version.
// A symbolic link that leaves the root fails on every platform and for every
// user, unlike a permission fixture.
func TestDetectVersionReportsAnUnreadableMarkerAsUnknown(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "controllers")
	if err := os.WriteFile(outside, []byte("memory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"cgroup.controllers", "memory"} {
		t.Run(marker, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Symlink(outside, filepath.Join(root, marker)); err != nil {
				t.Fatal(err)
			}
			got, err := DetectVersion(root)
			if err == nil || got != VersionUnknown {
				t.Fatalf("DetectVersion with an escaping %s = %q, %v; want unknown and an error", marker, got, err)
			}
		})
	}
}

func TestDetectVersionReportsAMissingRootAsAnError(t *testing.T) {
	got, err := DetectVersion(filepath.Join(t.TempDir(), "absent"))
	if err == nil || got != VersionUnknown {
		t.Fatalf("DetectVersion of a missing root = %q, %v; want unknown and an error", got, err)
	}
}
