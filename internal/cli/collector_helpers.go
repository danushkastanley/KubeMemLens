package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
)

func collectorUnavailableError(opts client.Options, description string, err error) error {
	return client.ConnectionError(opts, description, err)
}

func readPod(ctx context.Context, reader client.SnapshotReader, namespace, name string) (api.PodSnapshot, error) {
	if direct, ok := reader.(client.PodReader); ok {
		return direct.Pod(ctx, namespace, name)
	}
	pods, err := reader.Pods(ctx)
	if err != nil {
		return api.PodSnapshot{}, err
	}
	for _, pod := range pods {
		if pod.Namespace == namespace && pod.PodName == name {
			return pod, nil
		}
	}
	return api.PodSnapshot{}, &client.ReadError{Kind: client.ReadErrorNotFound, Operation: "get Pod"}
}

func withReadScope(opts client.Options, namespace string, allNamespaces bool) (client.Options, error) {
	if allNamespaces {
		opts.ReadScope = client.AllNamespacesScope()
		return opts, nil
	}
	scope, err := client.NamespaceScope(namespace)
	if err != nil {
		return client.Options{}, err
	}
	opts.ReadScope = scope
	return opts, nil
}

func formatAge(capturedAt time.Time) string {
	if capturedAt.IsZero() {
		return "-"
	}
	age := time.Since(capturedAt)
	if age < 0 {
		age = 0
	}
	age = age.Round(time.Second)
	if age < time.Minute {
		return fmt.Sprintf("%ds", int(age.Seconds()))
	}
	if age < time.Hour {
		return fmt.Sprintf("%dm", int(age.Minutes()))
	}
	if age < 24*time.Hour {
		return fmt.Sprintf("%dh", int(age.Hours()))
	}
	return fmt.Sprintf("%dd", int(age.Hours()/24))
}
