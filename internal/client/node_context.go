package client

import (
	"context"
	"fmt"
	"net/url"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
	"k8s.io/apimachinery/pkg/util/validation"
)

// NodeContextReader pages Node-only evidence separately from Pod data. The
// server authorises each request; a Node permission never grants contributors.
type NodeContextReader interface {
	NodeContexts(context.Context, string) (api.NodeContextList, error)
	NodeContext(context.Context, string) (api.NodeContextResource, error)
	NodeContextHistory(context.Context, string, string) (api.NodeContextHistory, error)
}

func (c *KubernetesAPIClient) NodeContexts(ctx context.Context, continuation string) (api.NodeContextList, error) {
	var page api.NodeContextList
	query := url.Values{"limit": {"100"}, "continue": {continuation}}
	if err := c.get(ctx, "list Node context", "/nodecontexts?"+query.Encode(), &page); err != nil {
		return page, err
	}
	if len(page.Items) > nodecontext.MaxPageRecords {
		return api.NodeContextList{}, readDecodeError("list Node context", fmt.Errorf("page exceeds Node record limit"))
	}
	return page, nil
}

func (c *KubernetesAPIClient) NodeContext(ctx context.Context, name string) (api.NodeContextResource, error) {
	var result api.NodeContextResource
	path, err := nodeContextPath(name)
	if err != nil {
		return result, err
	}
	err = c.get(ctx, "get Node context", path, &result)
	return result, err
}

func (c *KubernetesAPIClient) NodeContextHistory(ctx context.Context, name, continuation string) (api.NodeContextHistory, error) {
	var result api.NodeContextHistory
	path, err := nodeContextPath(name)
	if err != nil {
		return result, err
	}
	// One instance keeps even a maximum-width 61-point series below 1 MiB.
	query := url.Values{"limit": {"1"}, "continue": {continuation}}
	if err := c.get(ctx, "get Node context history", path+"/history?"+query.Encode(), &result); err != nil {
		return result, err
	}
	if len(result.Series) > 1 {
		return api.NodeContextHistory{}, readDecodeError("get Node context history", fmt.Errorf("history page exceeds one instance"))
	}
	for _, series := range result.Series {
		if len(series.Points) > nodecontext.MaxHistoryPoints {
			return api.NodeContextHistory{}, readDecodeError("get Node context history", fmt.Errorf("history exceeds point limit"))
		}
	}
	return result, nil
}

func nodeContextPath(name string) (string, error) {
	if len(validation.IsDNS1123Subdomain(name)) != 0 {
		return "", fmt.Errorf("valid Node name is required")
	}
	return "/nodecontexts/" + url.PathEscape(name), nil
}
