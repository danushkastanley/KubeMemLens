package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"k8s.io/client-go/rest"
)

const maxOutputBytes = 16 << 20

func read(ctx context.Context, config *rest.Config, opts options, output io.Writer) error {
	endpoint, err := url.Parse(config.Host)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || config.Insecure {
		return errors.New("verified Kubernetes TLS is required")
	}
	scope, err := client.NamespaceScope(opts.namespace)
	if err != nil {
		return err
	}
	reader, err := client.NewKubernetesAPIClient(config, scope, 8*time.Second)
	if err != nil {
		return err
	}
	var value any
	switch opts.operation {
	case "status":
		var store api.DebugStore
		store, err = reader.DebugStore(ctx)
		value = struct {
			Store api.DebugStore `json:"store"`
		}{store}
	case "containers":
		var rows []api.ContainerSnapshot
		rows, err = reader.Containers(ctx)
		if len(rows) > 2000 {
			return errors.New("qualification namespace container bound exceeded")
		}
		items := make([]api.ContainerMemory, len(rows))
		for index, row := range rows {
			items[index].Snapshot = row
		}
		value = struct {
			Items []api.ContainerMemory `json:"items"`
		}{items}
	case "node":
		value, err = reader.NodeContext(ctx, opts.node)
	case "history":
		value, err = reader.NodeContextHistory(ctx, opts.node, "")
	default:
		return errors.New("unsupported qualification operation")
	}
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(encoded) > maxOutputBytes {
		return errors.New("qualification API output bound exceeded")
	}
	_, err = output.Write(append(encoded, '\n'))
	return err
}
