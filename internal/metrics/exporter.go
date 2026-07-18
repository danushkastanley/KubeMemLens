package metrics

import (
	"fmt"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

const ContentType = "application/openmetrics-text; version=1.0.0; charset=utf-8"

type Source interface {
	ListContainers(now time.Time, ttl time.Duration) []api.ContainerSnapshot
	ListPods(now time.Time, ttl time.Duration) []api.PodSnapshot
	ListNamespaces(now time.Time, ttl time.Duration) []api.NamespaceSnapshot
	Debug(now time.Time, ttl time.Duration) api.DebugStore
	LatestByNode(now time.Time) map[string]time.Time
}

type Exporter struct {
	Source Source
	TTL    time.Duration
	Now    func() time.Time
	Opts   Options
}

func (e Exporter) Render() (string, error) {
	if e.Source == nil {
		return "", fmt.Errorf("metrics source is required")
	}
	now := time.Now().UTC()
	if e.Now != nil {
		now = e.Now().UTC()
	}
	opts := e.Opts
	if opts == (Options{}) {
		opts = DefaultOptions()
	}

	renderer := newRenderer()
	renderer.render(e.Source, now, e.TTL, opts)
	return renderer.String(), nil
}
