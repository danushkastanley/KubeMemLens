package traceclient

import (
	"context"
	"slices"
	"time"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *Client) Discover(ctx context.Context) error {
	data, status, err := c.control(ctx, "GET", apiPrefix, nil, 200, maxControlBytes)
	if status == 404 {
		return failure(Unavailable)
	}
	if err != nil {
		return err
	}
	var resources metav1.APIResourceList
	if decode(data, &resources) != nil || resources.GroupVersion != "tracing.kubememlens.io/v1alpha1" || resources.Kind != "APIResourceList" {
		return failure(Protocol)
	}
	wanted := map[string][]string{"traces": {"create", "get", "delete"}, "traces/stream": {"get"}, "tracepreflights": {"create"}}
	seen := map[string]bool{}
	for _, r := range resources.APIResources {
		verbs, needed := wanted[r.Name]
		if !needed {
			continue
		}
		if seen[r.Name] || !r.Namespaced {
			return failure(Protocol)
		}
		for _, verb := range verbs {
			if !slices.Contains(r.Verbs, verb) {
				return failure(Unavailable)
			}
		}
		seen[r.Name] = true
	}
	if len(seen) != len(wanted) {
		return failure(Unavailable)
	}
	return nil
}

func (c *Client) Preflight(ctx context.Context, selection Selection, intent Intent) (Plan, error) {
	body, err := requestData(selection, intent)
	if err != nil {
		return Plan{}, err
	}
	if err := c.Discover(ctx); err != nil {
		return Plan{}, err
	}
	data, _, err := c.control(ctx, "POST", apiPrefix+"/namespaces/"+selection.Namespace+"/tracepreflights", body, 200, maxControlBytes)
	if err != nil {
		return Plan{}, err
	}
	var doc admission.PreflightDocument
	if decode(data, &doc) != nil || doc.SchemaVersion != 1 || doc.Kind != "TracePreflight" || doc.RequestSchemaVersion != 2 || doc.CheckedAt.IsZero() || doc.ResourceQualified || doc.TraceKind != intent.Kind || doc.Paths != intent.Paths || doc.Node.Validate(intent.Kind) != nil {
		return Plan{}, failure(Protocol)
	}
	b := doc.Bounds
	if time.Duration(b.DurationNanos) != intent.Bounds.Duration || b.Events != intent.Bounds.Events || b.OutputBytes != intent.Bounds.OutputBytes || b.MapBytes != intent.Bounds.MapBytes || b.PathBytes != intent.Bounds.PathBytes {
		return Plan{}, failure(Protocol)
	}
	return Plan{client: c, selection: selection, intent: intent, document: doc}, nil
}

// PreflightReport returns an owned copy. Editing a presentation copy cannot
// alter the intent or artefacts subsequently checked by Create and Watch.
func (p Plan) PreflightReport() admission.PreflightDocument {
	doc := p.document
	doc.Node.Baseline.Checks = slices.Clone(doc.Node.Baseline.Checks)
	return doc
}
