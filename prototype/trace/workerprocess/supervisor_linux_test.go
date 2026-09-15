package workerprocess

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/prototype/trace/workeripc"
)

func requestFixture(t *testing.T, duration time.Duration) workeripc.Request {
	t.Helper()
	now := time.Now().UTC()
	bounds := trace.DefaultBounds()
	bounds.Duration = duration
	spec, err := trace.NewSpecification(trace.Cache, trace.TargetIdentity{Namespace: "tenant", PodName: "test", PodUID: "pod", ContainerName: "work", ContainerID: strings.Repeat("a", 64), ContainerStartedAt: now.Add(-time.Minute), NodeUID: "node", CgroupID: 1}, trace.OmitPaths, bounds)
	if err != nil {
		t.Fatal(err)
	}
	return workeripc.Request{Specification: spec, IssuedAt: now, Deadline: now.Add(duration), ManifestSHA256: strings.Repeat("b", 64)}
}

func child(t *testing.T, mode string) *exec.Cmd {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestWorkerProcess$", "--", mode)
	// The race runtime otherwise adds a one-second sleep to os.Exit, longer
	// than the production worker's bounded exit grace. Keep race detection on.
	cmd.Env = []string{"KML_PROCESS_FIXTURE=1", "GORACE=atexit_sleep_ms=0"}
	target, err := os.Open("/dev/null")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = target.Close() })
	cmd.ExtraFiles = []*os.File{target}
	return cmd
}

// This subprocess exercises the real private pipes, process launch and wait
// path. It loads no BPF object and does not stand in for kernel qualification.
func TestWorkerProcess(t *testing.T) {
	if os.Getenv("KML_PROCESS_FIXTURE") != "1" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	if _, err := os.NewFile(3, "inherited-target").Stat(); err != nil {
		os.Exit(30)
	}
	request, err := workeripc.ReadRequest(os.Stdin)
	if err != nil {
		os.Exit(31)
	}
	if mode == "startup-hang" {
		time.Sleep(time.Minute)
		os.Exit(32)
	}
	if mode == "ignore-term" || mode == "terminal-hang" {
		signal.Ignore(syscall.SIGTERM)
	}
	if mode == "malformed" {
		_, _ = os.Stdout.Write([]byte{255, 255, 255, 255})
		time.Sleep(time.Minute)
		os.Exit(33)
	}
	writer, err := workeripc.NewWriter(os.Stdout, request)
	if err != nil || writer.Ready() != nil {
		os.Exit(34)
	}
	if writer.CacheActivity(trace.CacheActivity{ObservedAt: time.Now().UTC(), Operation: trace.CacheAdd, Pages: 1}) != nil {
		os.Exit(35)
	}
	if mode == "ignore-term" {
		time.Sleep(time.Minute)
		os.Exit(36)
	}
	if mode == "expiry-result" || mode == "expiry-slow-result" || mode == "expiry-bad-result" || mode == "expiry-late-event" {
		deadline, stop := context.WithDeadline(context.Background(), request.Deadline)
		<-deadline.Done()
		stop()
		if mode == "expiry-slow-result" {
			// Model the measured serial kernel-close latency beyond 500 ms.
			time.Sleep(650 * time.Millisecond)
		}
		if mode == "expiry-bad-result" {
			_, _ = os.Stdout.Write([]byte{255, 255, 255, 255})
			os.Exit(0)
		}
		if mode == "expiry-late-event" {
			_ = writer.CacheActivity(trace.CacheActivity{ObservedAt: request.IssuedAt, Operation: trace.CacheAdd, Pages: 1})
		}
		one, zero := uint64(1), uint64(0)
		if mode == "expiry-late-event" {
			one = 2
		}
		if writer.Finish(trace.Result{Version: trace.ContractVersion, StartedAt: request.IssuedAt, EndedAt: request.Deadline, Termination: trace.Expired, Counts: trace.Counts{Produced: &one, Sampled: &zero, Lost: &zero, Rejected: &zero}, Incomplete: true}) != nil {
			os.Exit(40)
		}
		os.Exit(0)
	}
	if writer.Finish(trace.Result{Version: trace.ContractVersion, Termination: trace.Cancelled, Incomplete: true}) != nil {
		os.Exit(37)
	}
	if mode == "terminal-hang" {
		_ = os.Stdout.Close()
		time.Sleep(time.Minute)
		os.Exit(38)
	}
	if mode == "nonzero-exit" {
		os.Exit(39)
	}
	os.Exit(0)
}

type outputFixture struct {
	events  int
	onEvent func()
}

func (*outputFixture) FileActivity(trace.FileActivity) error {
	return errors.New("unexpected file event")
}
func (o *outputFixture) CacheActivity(trace.CacheActivity) error {
	o.events++
	if o.onEvent != nil {
		o.onEvent()
	}
	return nil
}
func (*outputFixture) OOMDecision(trace.OOMDecision) error { return errors.New("unexpected OOM event") }

func assertReaped(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if cmd.ProcessState == nil || cmd.ProcessState.Pid() <= 0 {
		t.Fatal("process not reaped")
	}
	if err := syscall.Kill(cmd.ProcessState.Pid(), 0); err != syscall.ESRCH {
		t.Fatal("worker process remains live")
	}
}

func TestSuccessfulStreamWaitsForWorkerExit(t *testing.T) {
	cmd := child(t, "success")
	output := &outputFixture{}
	result, err := Run(context.Background(), cmd, requestFixture(t, 10*time.Second), output)
	if err != nil || result.Termination != trace.Cancelled || output.events != 1 {
		t.Fatal("valid worker did not complete")
	}
	assertReaped(t, cmd)
}

func TestInvalidOrUnfinishedWorkersAreReaped(t *testing.T) {
	for _, mode := range []string{"malformed", "nonzero-exit", "terminal-hang"} {
		t.Run(mode, func(t *testing.T) {
			cmd := child(t, mode)
			start := time.Now()
			_, err := Run(context.Background(), cmd, requestFixture(t, 10*time.Second), &outputFixture{})
			if err != ErrWorker || time.Since(start) > 4*time.Second {
				t.Fatal("failed worker was accepted or exceeded teardown bound")
			}
			assertReaped(t, cmd)
		})
	}
}

func TestCancellationEscalatesToKillAndReaps(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := child(t, "ignore-term")
	output := &outputFixture{onEvent: cancel}
	start := time.Now()
	_, err := Run(ctx, cmd, requestFixture(t, 10*time.Second), output)
	if err != ErrWorker || output.events != 1 || time.Since(start) > 4*time.Second {
		t.Fatal("cancellation did not bound worker")
	}
	assertReaped(t, cmd)
	status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatal("uncooperative worker not killed")
	}
}

func TestStartupAndExecutionDeadlines(t *testing.T) {
	for _, duration := range []time.Duration{200 * time.Millisecond, 10 * time.Second} {
		t.Run(duration.String(), func(t *testing.T) {
			cmd := child(t, "startup-hang")
			start := time.Now()
			_, err := Run(context.Background(), cmd, requestFixture(t, duration), &outputFixture{})
			bound := duration
			if bound > startupTimeout {
				bound = startupTimeout
			}
			if err != ErrWorker || time.Since(start) > bound+2*time.Second {
				t.Fatal("startup/deadline bound not enforced")
			}
			assertReaped(t, cmd)
		})
	}
}

func TestOutputPanicStillReapsWorker(t *testing.T) {
	cmd := child(t, "ignore-term")
	output := &outputFixture{onEvent: func() { panic("private callback contents") }}
	_, err := Run(context.Background(), cmd, requestFixture(t, 10*time.Second), output)
	if err != ErrWorker {
		t.Fatal("output panic was not contained")
	}
	assertReaped(t, cmd)
}

func TestUnconfirmedReapCannotReturnOrdinaryEngineError(t *testing.T) {
	defer func() {
		if recover() != ErrCleanupUnconfirmed {
			t.Fatal("unknown cleanup allowed a normal return")
		}
	}()
	_ = awaitReap(make(chan error), time.Millisecond)
}

func TestExpiredRequestDoesNotStartProcess(t *testing.T) {
	cmd := child(t, "success")
	r := requestFixture(t, time.Second)
	r.IssuedAt = r.IssuedAt.Add(-2 * time.Second)
	r.Deadline = r.Deadline.Add(-2 * time.Second)
	_, err := Run(context.Background(), cmd, r, &outputFixture{})
	if err != ErrWorker || cmd.Process != nil {
		t.Fatal("expired work started")
	}
}

func TestRetainedCallbackKeepsCleanupUnconfirmed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := child(t, "ignore-term")
	release := make(chan struct{})
	defer close(release)
	output := &outputFixture{onEvent: func() { cancel(); <-release }}
	var failure any
	func() {
		defer func() { failure = recover() }()
		_, _ = Run(ctx, cmd, requestFixture(t, 10*time.Second), output)
	}()
	if failure != ErrCleanupUnconfirmed {
		t.Fatal("retained callback authorised normal lease release")
	}
	assertReaped(t, cmd)
}
