package client

import (
	"slices"

	"github.com/danushkastanley/kube-memlens/internal/traceclient"
	"k8s.io/client-go/rest"
)

// Freeze the selected endpoint, identity and trust configuration with the
// memory reader. Opening a trace must never reread a changed ambient context.
// Native credential and CA file rotation remain transport responsibilities.
func freezeTraceConfig(config *rest.Config) *rest.Config {
	c := rest.CopyConfig(config)
	c.CAData = slices.Clone(config.CAData)
	c.CertData = slices.Clone(config.CertData)
	c.KeyData = slices.Clone(config.KeyData)
	return c
}

func (c *KubernetesAPIClient) NewTraceClient() (*traceclient.Client, error) {
	if c == nil || c.traceConfig == nil {
		return nil, &traceclient.Error{Kind: traceclient.Configuration}
	}
	return traceclient.New(c.traceConfig)
}
