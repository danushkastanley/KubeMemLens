package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
	"github.com/danushkastanley/kube-memlens/internal/tracepreflight"
	"github.com/danushkastanley/kube-memlens/prototype/trace/admissionapi"
	"github.com/danushkastanley/kube-memlens/prototype/trace/nodebinding"
	"github.com/danushkastanley/kube-memlens/prototype/trace/targetfs"
)

// Only this test binary contains the memory engine. Production entrypoints can
// select only an independently accepted worker; no flags select this fixture.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "_probe" {
		if run(context.Background(), os.Args[1:], os.Stdout, os.Stderr) != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	if os.Getenv("KML_STREAM_QUALIFICATION") == "owned-local-kind-contract-fixture" && len(os.Args) > 1 {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		var err error
		switch os.Args[1] {
		case "binding-node":
			err = runBindingNodeRuntime(ctx, os.Args[2:], os.Stderr, qualificationRuntime{})
		case "admission-api":
			proxy, createErr := admissionapi.NewStreamProxy(map[trace.Kind]string{trace.Files: qualificationDigest(), trace.Cache: qualificationDigest(), trace.OOM: qualificationDigest()})
			if createErr != nil {
				os.Exit(2)
			}
			policy := admission.DefaultPolicy()
			policy.Paths = trace.ConfirmedPaths
			err = runAdmissionAPIConfigured(ctx, os.Args[2:], os.Stderr, proxy, policy)
		default:
			os.Exit(2)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "qualification service failed")
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func qualificationDigest() string {
	digest := sha256.Sum256([]byte("KubeMemLens contract fixture v1; no kernel incident programme"))
	return "sha256:" + hex.EncodeToString(digest[:])
}

type qualificationRuntime struct{}

func (qualificationRuntime) Prepare(context.Context, trace.Specification, targetfs.Handle) (nodebinding.Prepared, error) {
	engine, err := trace.NewEngine(qualificationAdapter{})
	return nodebinding.Prepared{StreamVersion: traceframe.Version, Engine: engine, EngineDigest: tracepreflight.Baseline().EngineDigest, ProgrammeDigest: qualificationDigest()}, err
}

type qualificationAdapter struct{}

func (qualificationAdapter) Run(ctx context.Context, spec trace.Specification, out trace.Output) (trace.Result, error) {
	start := time.Now().UTC()
	produced, zero := uint64(0), uint64(0)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var err error
loop:
	for {
		select {
		case <-ctx.Done():
			err = ctx.Err()
			break loop
		case <-ticker.C:
			produced++
			switch spec.Kind() {
			case trace.Files:
				path, _ := trace.NewSensitiveText("/CONTRACT-FIXTURE-ONLY/\x1bpath", 256)
				err = out.FileActivity(trace.FileActivity{ObservedAt: time.Now().UTC(), Operation: trace.FileRead, Path: path})
			case trace.Cache:
				err = out.CacheActivity(trace.CacheActivity{ObservedAt: time.Now().UTC(), Operation: trace.CacheAdd, Pages: 1})
			case trace.OOM:
				command, _ := trace.NewSensitiveText("fixture", 16)
				err = out.OOMDecision(trace.OOMDecision{ObservedAt: time.Now().UTC(), Scope: trace.OOMScopeUnknown, Command: command})
			}
			if err != nil {
				break loop
			}
		}
	}
	return trace.Result{Version: 1, StartedAt: start, EndedAt: time.Now().UTC(), Termination: trace.Expired, Counts: trace.Counts{Produced: &produced, Sampled: &zero, Lost: &zero, Rejected: &zero}}, err
}
