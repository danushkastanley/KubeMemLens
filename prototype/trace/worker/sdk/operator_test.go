package sdk

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cilium/ebpf"
	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
	"github.com/inspektor-gadget/inspektor-gadget/pkg/logger"
	"github.com/inspektor-gadget/inspektor-gadget/pkg/operators"
)

// Run in a Linux test container with all capabilities dropped and BPF denied by
// Docker's default seccomp policy. This test never invokes SDK Start or loads BPF.
func TestVerifiedObjectPreparationWithoutReaders(t *testing.T) {
	root := os.Getenv("KML_FILECACHE_OBJECTS")
	if root == "" {
		t.Skip("requires offline candidate objects")
	}
	object, err := os.ReadFile(filepath.Join(root, "files-"+runtime.GOARCH+"-1", "program.bpf.o"))
	if err != nil {
		t.Fatal(err)
	}
	sha := func(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
	id := filecache.ArtifactID{Kind: trace.Files, Architecture: runtime.GOARCH}
	oci, err := filecache.OCIManifest(id, object)
	if err != nil {
		t.Fatal(err)
	}
	m := filecache.Manifest{Version: 1, Kind: id.Kind, Architecture: id.Architecture,
		ObjectSHA256: sha(object), SourceSHA256: strings.Repeat("a", 64), OCIManifestSHA256: sha(oci),
		EngineCommit: filecache.EngineSourceCommit, BuilderDigest: filecache.BuilderDigest, EnginePatchSHA256: filecache.EnginePatchSHA256}
	manifest, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	key, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := filecache.NewVerifier(key, map[filecache.ArtifactID]string{id: sha(manifest)})
	if err != nil {
		t.Fatal(err)
	}
	p, err := verifier.Verify(id, manifest, ed25519.Sign(private, manifest), object, oci)
	if err != nil {
		t.Fatal(err)
	}
	target := trace.TargetIdentity{Namespace: "fixture", PodName: "target", PodUID: "uid", ContainerName: "worker", ContainerID: strings.Repeat("b", 64), ContainerStartedAt: time.Unix(1, 0), NodeUID: "node", CgroupID: 123}
	spec, err := trace.NewSpecification(trace.Files, target, trace.OmitPaths, trace.DefaultBounds())
	if err != nil {
		t.Fatal(err)
	}
	diagnostic := &diagnostics{}
	called := false
	gctx, instance, err := prepare(context.Background(), p, spec, 1000, logger.NewFromGenericLogger(diagnostic), func(context.Context, map[string]*ebpf.Map) error { called = true; return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer gctx.Cancel()
	defer instance.Close(gctx)
	if pre, ok := instance.(operators.PreStart); ok {
		if err := pre.PreStart(gctx); err != nil {
			t.Fatal(err)
		}
	}
	if called || len(gctx.GetDataSources()) != 0 || diagnostic.failed.Load() {
		t.Fatal("preparation activated collection or created a generic data reader")
	}
	if _, ok := gctx.GetVar(operators.MapPrefix + "events"); ok {
		t.Fatal("preparation created a kernel event map")
	}
	value, ok := gctx.GetVar(operators.MapSpecPrefix + "events")
	mapSpec, typed := value.(*ebpf.MapSpec)
	if !ok || !typed || mapSpec.Type != ebpf.RingBuf || mapSpec.MaxEntries != 262144 {
		t.Fatal("SDK altered the fixed ring specification")
	}
}

func TestDiagnosticsRetainSeverityWithoutMessageContent(t *testing.T) {
	d := &diagnostics{}
	d.Logf(logger.WarnLevel, "private %s", "fixture-value")
	if !d.warned.Load() || d.failed.Load() {
		t.Fatal("warning state lost")
	}
	d.Log(logger.ErrorLevel, "private failure")
	if !d.failed.Load() {
		t.Fatal("SDK failure hidden")
	}
}
