package admissionapi

import (
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

func TestInstalledKindsKeepIndependentStreamVersions(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	programmes := map[trace.Kind]StreamProgramme{
		trace.Files: {Digest: digest, Version: traceframe.AggregateVersion},
		trace.OOM:   {Digest: digest, Version: traceframe.OOMVersion},
	}
	proxy, err := NewStreamProxyProgrammes(programmes)
	if err != nil || proxy.programmes[trace.Files].Version != 2 || proxy.programmes[trace.OOM].Version != 3 {
		t.Fatal("installation formats were conflated")
	}
	programmes[trace.OOM] = StreamProgramme{Digest: digest, Version: 1}
	if proxy.programmes[trace.OOM].Version != 3 {
		t.Fatal("caller changed retained installation format")
	}
	for _, programme := range []StreamProgramme{{Digest: digest, Version: 2}, {Digest: digest, Version: 4}, {Digest: "unaccepted", Version: 3}} {
		if _, err := NewStreamProxyProgrammes(map[trace.Kind]StreamProgramme{trace.OOM: programme}); err == nil {
			t.Fatal("invalid OOM installation format accepted")
		}
	}
}
