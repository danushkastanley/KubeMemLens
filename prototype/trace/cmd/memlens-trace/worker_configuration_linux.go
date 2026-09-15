package main

import (
	"context"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
	"github.com/danushkastanley/kube-memlens/prototype/trace/admissionapi"
	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
	"github.com/danushkastanley/kube-memlens/prototype/trace/workerinstall"
	"github.com/danushkastanley/kube-memlens/prototype/trace/workerruntime"
)

func configureWorker(ctx context.Context, acceptance, executable, bundle string) (installedRuntime, error) {
	policy, err := workerinstall.ReadFile(acceptance)
	if err != nil {
		return nil, err
	}
	return workerruntime.New(ctx, policy, executable, bundle)
}

func configureStream(acceptance string, base admission.Policy) (*admissionapi.StreamProxy, admission.Policy, error) {
	policy, err := workerinstall.ReadFile(acceptance)
	if err != nil {
		return nil, base, err
	}
	programmes := make(map[trace.Kind]string)
	for _, kind := range []trace.Kind{trace.Files, trace.Cache} {
		for _, arch := range []string{"arm64", "amd64"} {
			if _, err := policy.ManifestSHA256(filecache.ArtifactID{Kind: kind, Architecture: arch}); err == nil {
				programmes[kind] = policy.ProgrammeDigest()
			}
		}
	}
	proxy, err := admissionapi.NewStreamProxyVersion(programmes, traceframe.AggregateVersion)
	if err != nil {
		return nil, base, err
	}
	base.EngineDigest = policy.EngineDigest()
	return proxy, base, nil
}
