package workerruntime

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
	"github.com/danushkastanley/kube-memlens/prototype/trace/internal/testworker"
	"github.com/danushkastanley/kube-memlens/prototype/trace/targetfs"
	"github.com/danushkastanley/kube-memlens/prototype/trace/workercontainment"
	"github.com/danushkastanley/kube-memlens/prototype/trace/workerinstall"
	"github.com/danushkastanley/kube-memlens/prototype/trace/workeripc"
)

// Only this test binary implements the protocol fixture. It never starts the
// SDK or loads incident BPF; production Runtime.New fixes the real exporter.
func TestMain(m *testing.M) {
	if os.Args[0] == "memlens-filecache-worker" {
		os.Exit(fixtureWorker())
	}
	code := m.Run()
	if testworker.Cleanup() != nil {
		code = 1
	}
	os.Exit(code)
}

func fixtureWorker() int {
	if testworker.Baseline() != nil || workercontainment.Restrict() != nil || testworker.CheckBaseline() != nil || len(os.Environ()) != 1 || os.Getenv("GOTRACEBACK") != "none" {
		return 20
	}
	policy, err := workerinstall.ReadDescriptor(os.NewFile(6, "policy"))
	if err != nil || policy.VerifyRunning(os.NewFile(4, "image")) != nil {
		return 21
	}
	root, err := os.OpenRoot("/proc/self/fd/5")
	if err != nil {
		return 22
	}
	defer root.Close()
	request, err := workeripc.ReadRequest(os.Stdin)
	if err != nil {
		return 23
	}
	id := filecache.ArtifactID{Kind: request.Specification.Kind(), Architecture: runtime.GOARCH}
	digest, err := policy.ManifestSHA256(id)
	if err != nil || digest != request.ManifestSHA256 {
		return 24
	}
	if _, err := policy.Programme(root, id); err != nil {
		return 25
	}
	if _, err := os.NewFile(3, "target").Stat(); err != nil {
		return 26
	}
	writer, err := workeripc.NewWriter(os.Stdout, request)
	if err != nil || writer.Ready() != nil {
		return 27
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()
	if writer.CacheActivity(trace.CacheActivity{ObservedAt: time.Now().UTC(), Operation: trace.CacheAdd, Pages: 1}) != nil {
		return 28
	}
	if request.Specification.Target().PodName == "wait-for-close" {
		<-ctx.Done()
	}
	one, zero := uint64(1), uint64(0)
	if writer.Finish(trace.Result{Version: trace.ContractVersion, Termination: trace.Cancelled, Counts: trace.Counts{Produced: &one, Sampled: &zero, Lost: &zero, Rejected: &zero}, Incomplete: true}) != nil {
		return 29
	}
	return 0
}

func checksum(data []byte) string { value := sha256.Sum256(data); return hex.EncodeToString(value[:]) }

func fixtureRuntime(t *testing.T) *Runtime {
	t.Helper()
	bundle := os.Getenv("KML_REVIEW_BUNDLE")
	if bundle == "" {
		t.Skip("requires retained signed review bundle")
	}
	indexData, err := os.ReadFile(filepath.Join(bundle, "candidate-index.json"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(filepath.Join(bundle, "signing-public-key.bin"))
	if err != nil {
		t.Fatal(err)
	}
	var index filecache.CandidateIndex
	if json.Unmarshal(indexData, &index) != nil {
		t.Fatal("invalid fixture index")
	}
	path, err := testworker.StaticBinary()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	release := workerinstall.EngineRelease{SourceCommit: filecache.EngineSourceCommit, PatchSHA256: filecache.EnginePatchSHA256, Workers: map[string]string{runtime.GOARCH: checksum(data)}}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(release)
	if err != nil {
		t.Fatal(err)
	}
	config := workerinstall.Configuration{Version: 1, Engine: release, EnginePublicKey: public, EngineSignature: ed25519.Sign(private, encoded), ProgrammeIndexSHA256: checksum(indexData), ProgrammePublicKey: key}
	for _, entry := range index.Programmes {
		if entry.Architecture == runtime.GOARCH {
			config.Programmes = append(config.Programmes, entry)
		}
	}
	encoded, err = json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := workerinstall.Parse(encoded)
	if err != nil {
		t.Fatal(err)
	}
	r, err := New(context.Background(), policy, path, bundle)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = r.Close(ctx)
	})
	return r
}

type fixtureHandle struct {
	target trace.TargetIdentity
	closed atomic.Bool
}

func (h *fixtureHandle) Target() trace.TargetIdentity { return h.target }
func (h *fixtureHandle) Check(ctx context.Context) error {
	if h.closed.Load() || ctx.Err() != nil {
		return ErrRuntime
	}
	return nil
}
func (h *fixtureHandle) Close() error { h.closed.Store(true); return nil }

func fixtureSpec(t *testing.T, pod string) (trace.Specification, *fixtureHandle) {
	t.Helper()
	target := trace.TargetIdentity{Namespace: "fixture", PodName: pod, PodUID: "uid", ContainerName: "worker", ContainerID: strings.Repeat("a", 64), ContainerStartedAt: time.Now().UTC().Add(-time.Minute), NodeUID: "node", CgroupID: 123}
	spec, err := trace.NewSpecification(trace.Cache, target, trace.OmitPaths, trace.DefaultBounds())
	if err != nil {
		t.Fatal(err)
	}
	return spec, &fixtureHandle{target: target}
}

func fixtureExporter(exported chan *os.File) func(context.Context, targetfs.Handle) (*os.File, error) {
	return func(context.Context, targetfs.Handle) (*os.File, error) {
		file, err := os.Open("/dev/null")
		if err == nil {
			exported <- file
		}
		return file, err
	}
}

type fixtureOutput struct {
	received chan struct{}
	events   atomic.Int64
}

func (*fixtureOutput) FileActivity(trace.FileActivity) error { return ErrRuntime }
func (*fixtureOutput) OOMDecision(trace.OOMDecision) error   { return ErrRuntime }
func (o *fixtureOutput) CacheActivity(event trace.CacheActivity) error {
	if event.Pages != 1 || event.Operation != trace.CacheAdd {
		return ErrRuntime
	}
	o.events.Add(1)
	if o.received != nil {
		o.received <- struct{}{}
	}
	return nil
}
