package metrics

import (
	"errors"
	"fmt"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

const ContentType = "application/openmetrics-text; version=1.0.0; charset=utf-8"

// Format is the exposition dialect. The two differ in how a counter is
// declared: OpenMetrics names the family without the _total suffix its samples
// carry, while the classic Prometheus text format names the sample itself.
type Format int

const (
	OpenMetrics Format = iota
	PrometheusText
)

var ErrOutputTooLarge = errors.New("metrics output exceeds the configured maximum")

type Source interface {
	ListContainers(now time.Time, ttl time.Duration) []api.ContainerSnapshot
	ListPods(now time.Time, ttl time.Duration) []api.PodSnapshot
	ListNamespaces(now time.Time, ttl time.Duration) []api.NamespaceSnapshot
	Debug(now time.Time, ttl time.Duration) api.DebugStore
	LatestByNode(now time.Time) map[string]time.Time
}

type Exporter struct {
	Source      Source
	TTL         time.Duration
	Now         func() time.Time
	Opts        Options
	MaxBytes    int
	Format      Format
	Operational *Operational
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

	// Operational series follow the entity levels in the exposition, so they
	// are rendered first and their size is held back from the level budget.
	tail := newRenderer(e.MaxBytes, e.Format)
	if e.Operational != nil {
		tail.operational(*e.Operational)
	}
	if tail.b.err != nil {
		return "", tail.b.err
	}
	renderer := newRenderer(e.MaxBytes, e.Format)
	renderer.render(e.Source, now, e.TTL, opts, tail.b.Len()+len(eofLine))
	_, _ = renderer.b.WriteString(tail.b.String())
	return renderer.String()
}
