package sdk

import (
	"errors"
	"os"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

// The native regression mounts an empty directory at /sys/kernel/btf. Object
// preparation must not read kernel BTF merely to format absent data sources.
// This does not call Start or remove BTF requirements for actual attachments.
func TestRawPreparationDoesNotRequireFormatterKernelBTF(t *testing.T) {
	if os.Getenv("KML_NO_KERNEL_BTF") != "1" {
		t.Skip("requires an explicitly hidden kernel BTF directory")
	}
	if _, err := os.Stat("/sys/kernel/btf/vmlinux"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("kernel BTF absence was not established")
	}
	for _, kind := range []trace.Kind{trace.Files, trace.Cache, trace.OOM} {
		t.Run(string(kind), func(t *testing.T) {
			environment := "KML_FILECACHE_OBJECTS"
			if kind == trace.OOM {
				environment = "KML_OOM_OBJECTS"
			}
			diagnostic := verifyObjectPreparation(t, kind, environment)
			if diagnostic.warned.Load() {
				t.Fatal("raw preparation attempted unavailable display formatting")
			}
		})
	}
}
