package client

import (
	"context"
	"fmt"
	"net/url"

	"github.com/danushkastanley/kube-memlens/internal/aggregate"
	"github.com/danushkastanley/kube-memlens/internal/api"
)

const (
	clientContainerPageSize = 500
	clientMaxContainers     = 100_000
)

type CurrentSnapshot struct {
	Namespaces []api.NamespaceSnapshot
	Workloads  []api.WorkloadSnapshot
	Pods       []api.PodSnapshot
	Containers []api.ContainerSnapshot
}

type CurrentSnapshotReader interface {
	CurrentSnapshot(ctx context.Context) (CurrentSnapshot, error)
}

type pageGetter func(context.Context, string, any) error

func loadCurrentSnapshot(ctx context.Context, get pageGetter) (CurrentSnapshot, error) {
	containers, err := loadContainerPages(ctx, get)
	if err != nil {
		return CurrentSnapshot{}, err
	}
	pods := aggregate.Pods(containers)
	return CurrentSnapshot{
		Containers: containers,
		Pods:       pods,
		Namespaces: aggregate.Namespaces(pods),
		Workloads:  aggregate.Workloads(pods),
	}, nil
}

func loadContainerPages(ctx context.Context, get pageGetter) ([]api.ContainerSnapshot, error) {
	items := make([]api.ContainerSnapshot, 0, clientContainerPageSize)
	continuation := ""
	seen := map[string]struct{}{}
	for {
		path := fmt.Sprintf("/api/v1/pages/containers?limit=%d", clientContainerPageSize)
		if continuation != "" {
			path += "&continue=" + url.QueryEscape(continuation)
		}
		var page api.ContainerPage
		if err := get(ctx, path, &page); err != nil {
			return nil, err
		}
		if len(page.Items) > clientContainerPageSize {
			return nil, fmt.Errorf("collector returned %d containers in one page; maximum is %d", len(page.Items), clientContainerPageSize)
		}
		if len(items)+len(page.Items) > clientMaxContainers {
			return nil, fmt.Errorf("collector response exceeds client maximum of %d containers", clientMaxContainers)
		}
		items = append(items, page.Items...)
		if page.Continue == "" {
			return items, nil
		}
		if len(page.Items) == 0 {
			return nil, fmt.Errorf("collector returned an empty page with a continuation token")
		}
		if _, duplicate := seen[page.Continue]; duplicate {
			return nil, fmt.Errorf("collector repeated a continuation token")
		}
		seen[page.Continue] = struct{}{}
		continuation = page.Continue
	}
}
