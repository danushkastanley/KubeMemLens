package tracepreflight

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

type fixtureProber struct {
	checks map[ID]Check
	calls  []ID
}

func (p *fixtureProber) Probe(_ context.Context, id ID) Check {
	p.calls = append(p.calls, id)
	if c, ok := p.checks[id]; ok {
		return c
	}
	return Check{ID: id, State: Supported, Reason: Available}
}

func checkerFixture(t *testing.T, prober Prober) *Checker {
	t.Helper()
	c, err := New(prober, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestMissingPrerequisiteCannotStartKernelProbes(t *testing.T) {
	for _, reason := range []Reason{BTFMissing, CapabilityMissing, PolicyDenied, EngineMismatch, OwnedOrphan} {
		t.Run(string(reason), func(t *testing.T) {
			id := map[Reason]ID{BTFMissing: BTF, CapabilityMissing: Capabilities, PolicyDenied: SecurityPolicy, EngineMismatch: EngineIdentity, OwnedOrphan: Ownership}[reason]
			p := &fixtureProber{checks: map[ID]Check{id: {ID: id, State: Unsupported, Reason: reason}}}
			r := checkerFixture(t, p).Run(context.Background())
			if r.State != Unsupported {
				t.Fatal("missing prerequisite reported supported")
			}
			for _, call := range p.calls {
				if needsKernelProbe(call) {
					t.Fatalf("kernel probe %s ran", call)
				}
			}
			if _, err := Encode(r); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSupportedAndDegradedRemainDistinct(t *testing.T) {
	for _, state := range []State{Supported, Degraded} {
		p := &fixtureProber{checks: map[ID]Check{}}
		if state == Degraded {
			p.checks[BPFFS] = Check{ID: BPFFS, State: Degraded, Reason: BPFFSAbsent}
		}
		r := checkerFixture(t, p).Run(context.Background())
		if r.State != state || len(p.calls) != len(Baseline().Checks) {
			t.Fatalf("state or probe coverage changed: %s", r.State)
		}
		if _, err := Encode(r); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInvalidAdapterOutputIsDiscarded(t *testing.T) {
	for _, bad := range []Check{
		{ID: Kernel, State: Supported, Reason: KernelUnsupported},
		{ID: Kernel, State: Supported, Reason: Available, Value: strings.Repeat("a", MaxValueBytes+1)},
		{ID: Kernel, State: Supported, Reason: Available, Value: "secret\x1b[2J"},
		{ID: "unreviewed", State: Supported, Reason: Available},
	} {
		p := &fixtureProber{checks: map[ID]Check{Kernel: bad}}
		r := checkerFixture(t, p).Run(context.Background())
		if r.State != Unsupported {
			t.Fatal("invalid adapter output accepted")
		}
		for _, check := range r.Checks {
			if check.ID == Kernel && (check.Reason != ProbeInvalid || check.Value != "") {
				t.Fatal("invalid output retained")
			}
		}
		if _, err := Encode(r); err != nil {
			t.Fatal(err)
		}
	}
}

type waitingProber struct {
	once    sync.Once
	started chan struct{}
}

func (p *waitingProber) Probe(ctx context.Context, id ID) Check {
	p.once.Do(func() { close(p.started) })
	<-ctx.Done()
	return Check{ID: id, State: Supported, Reason: Available}
}

func TestConcurrentCheckIsRejectedAndCancellationInvalidatesSuccess(t *testing.T) {
	p := &waitingProber{started: make(chan struct{})}
	c := checkerFixture(t, p)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	completed := make(chan Report, 1)
	go func() { completed <- c.Run(ctx) }()
	<-p.started
	busy := c.Run(context.Background())
	if len(busy.Checks) != 1 || busy.Checks[0].Reason != ProbeBusy {
		t.Fatal("concurrent probe admitted")
	}
	if _, err := Encode(busy); err != nil {
		t.Fatal(err)
	}
	cancel()
	result := <-completed
	if result.State != Degraded {
		t.Fatal("cancelled probe returned success")
	}
	for _, check := range result.Checks {
		if check.Reason != ProbeTimeout {
			t.Fatal("cancelled result hid incomplete checks")
		}
	}
	if _, err := Encode(result); err != nil {
		t.Fatal(err)
	}
}

func TestReportCannotOmitFailuresOrChangeProfile(t *testing.T) {
	valid := checkerFixture(t, &fixtureProber{}).Run(context.Background())
	for _, mutate := range []func(*Report){
		func(r *Report) { r.Scope = "tracing-supported" },
		func(r *Report) { r.ProfileDigest = "sha256:other" },
		func(r *Report) { r.Checks = r.Checks[:1] },
		func(r *Report) { r.Checks = append(r.Checks, r.Checks[0]) },
		func(r *Report) { r.State = Unsupported },
		func(r *Report) { r.Checks[0].ID = "unknown" },
	} {
		r := valid
		r.Checks = append([]Check(nil), valid.Checks...)
		mutate(&r)
		if _, err := Encode(r); err == nil {
			t.Fatal("forged report accepted")
		}
	}
	p := Baseline()
	p.Checks[0] = "changed"
	if Baseline().Checks[0] != Platform || p.Digest() == Baseline().Digest() {
		t.Fatal("profile mutation escaped or did not change its digest")
	}
}
