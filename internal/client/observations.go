package client

import (
	"context"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/agentless"
	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/kube"
	"github.com/danushkastanley/kube-memlens/internal/observation"
)

func observationReader(opts Options, session EvidenceSession) (observation.Reader, error) {
	if session.Plan.Mode == capability.Deep {
		return deepObservationReader{reader: session.Reader}, nil
	}
	config, err := kube.BuildConfig(opts.Kubeconfig, opts.Context)
	if err != nil {
		return nil, err
	}
	bounds := agentless.Options{Timeout: opts.Timeout}
	if opts.ReadScope.AllNamespaces {
		return agentless.NewCluster(config, bounds)
	}
	return agentless.NewNamespace(config, opts.ReadScope.Namespace, bounds)
}

type deepObservationReader struct{ reader SnapshotReader }

// The common current query projects the original read contracts. It does not
// replace the collector client or change existing cgroup aggregation.
func (r deepObservationReader) Current(ctx context.Context) (observation.Batch, error) {
	pods, err := r.reader.Pods(ctx)
	if err != nil {
		return observation.Batch{}, err
	}
	namespaces, err := r.reader.Namespaces(ctx)
	if err != nil {
		return observation.Batch{}, err
	}
	workloads, err := r.reader.Workloads(ctx)
	if err != nil {
		return observation.Batch{}, err
	}
	nodes, err := r.reader.Nodes(ctx)
	if err != nil {
		return observation.Batch{}, err
	}
	at := time.Now().UTC()
	batch := observation.Batch{Mode: capability.Deep, ReceivedAt: at, Completeness: capability.Complete,
		Pods: []observation.Pod{}, Nodes: []observation.Node{}, Namespaces: []observation.Group{}, Workloads: []observation.Group{}}
	for _, pod := range pods {
		batch.Pods = append(batch.Pods, observation.FromDeepPod(pod, at))
		if pod.Completeness != "complete" {
			batch.Completeness = capability.Partial
		}
	}
	for _, value := range namespaces {
		batch.Namespaces = append(batch.Namespaces, observation.FromDeepNamespace(value, at))
	}
	for _, value := range workloads {
		batch.Workloads = append(batch.Workloads, observation.FromDeepWorkload(value, at))
	}
	for _, value := range nodes {
		batch.Nodes = append(batch.Nodes, observation.FromDeepNode(value, at))
	}
	return batch, nil
}
