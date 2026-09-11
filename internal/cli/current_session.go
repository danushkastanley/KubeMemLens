package cli

import (
	"context"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/client"
)

// Explicit deep/collector commands keep their original reads. Automatic
// Kubernetes commands use shared discovery to select the authorised source.
func currentSession(ctx context.Context, opts client.Options) (client.EvidenceSession, error) {
	return commandSession(ctx, opts, client.NewEvidenceSession)
}

func currentPodSession(ctx context.Context, opts client.Options, podName string) (client.EvidenceSession, error) {
	return commandSession(ctx, opts, func(ctx context.Context, opts client.Options) (client.EvidenceSession, error) {
		return client.NewPodEvidenceSession(ctx, opts, podName)
	})
}

func commandSession(ctx context.Context, opts client.Options, discover func(context.Context, client.Options) (client.EvidenceSession, error)) (client.EvidenceSession, error) {
	opts, err := opts.WithDefaults()
	if err != nil {
		return client.EvidenceSession{}, err
	}
	mode, err := client.ResolveMode(opts)
	if err != nil {
		return client.EvidenceSession{}, err
	}
	if opts.EvidenceMode != capability.Restricted && (opts.EvidenceMode == capability.Deep || mode != client.ConnectionModeKubernetesAPI) {
		reader, description, err := client.NewSnapshotReader(ctx, opts)
		return client.EvidenceSession{Reader: reader, Description: description, Plan: capability.Selection{Mode: capability.Deep}}, err
	}
	return discover(ctx, opts)
}
