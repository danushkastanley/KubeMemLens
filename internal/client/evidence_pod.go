package client

import (
	"context"
	"fmt"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"k8s.io/apimachinery/pkg/util/validation"
)

// NewPodEvidenceSession probes the requested deep Pod rather than requiring
// permission to list its namespace. Restricted queries still use their bounded
// namespace inventory and authorise every actual read.
func NewPodEvidenceSession(ctx context.Context, opts Options, podName string) (EvidenceSession, error) {
	if opts.ReadScope.AllNamespaces || len(validation.IsDNS1123Subdomain(podName)) != 0 {
		return EvidenceSession{}, fmt.Errorf("one namespace and a valid Pod name are required")
	}
	return newEvidenceSession(ctx, opts, podName)
}

func discoverDeepPod(ctx context.Context, reader SnapshotReader, namespace, name string) (capability.SourceState, error) {
	single, ok := reader.(PodReader)
	if !ok {
		return discoverDeep(ctx, reader)
	}
	pod, err := single.Pod(ctx, namespace, name)
	if err == nil {
		return deepSourceState([]api.PodSnapshot{pod}), nil
	}
	if IsNotFound(err) {
		// A missing object is not an absent API. Only the group check can
		// justify a source fallback after this 404.
		if healthErr := reader.Health(ctx); healthErr != nil {
			return evidenceFailure(healthErr), nil
		}
		return deepSourceState(nil), nil
	}
	return evidenceFailure(err), nil
}
