package kube

import (
	"context"
	"errors"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Intentionally omit messages, annotations, labels, reporting instance and action.
// They can contain arbitrary application or tenant data.
type markerEvent struct {
	Metadata       struct{ UID, Namespace string } `json:"metadata"`
	Regarding      changemarkers.Object            `json:"involvedObject"`
	Reason         string                          `json:"reason"`
	EventTime      metav1.MicroTime                `json:"eventTime"`
	FirstTimestamp metav1.Time                     `json:"firstTimestamp"`
	LastTimestamp  metav1.Time                     `json:"lastTimestamp"`
	Count          int32                           `json:"count"`
	Series         *struct {
		Count            int32            `json:"count"`
		LastObservedTime metav1.MicroTime `json:"lastObservedTime"`
	} `json:"series"`
}
type markerEvents struct {
	metav1.TypeMeta `json:",inline"`
	Metadata        struct {
		Continue string `json:"continue"`
	} `json:"metadata"`
	Items []markerEvent `json:"items"`
}

func (q *markerQuery) events(ctx context.Context, selected memoryhistory.Selection) ([]changemarkers.Marker, changemarkers.Coverage, bool, error) {
	return q.eventsForNamespace(ctx, selected.Request.Namespace)
}

func (q *markerQuery) eventsForNamespace(ctx context.Context, namespace string) ([]changemarkers.Marker, changemarkers.Coverage, bool, error) {
	a := eventAccess(namespace)
	if err := q.authorize(ctx, a); err != nil {
		if markerDenied(err) {
			return nil, changemarkers.Denied, false, nil
		}
		return nil, changemarkers.Unavailable, false, err
	}
	var page markerEvents
	priorValidation := q.validateJSON
	defer func() { q.validateJSON = priorValidation }()
	q.validateJSON = func(body []byte) error { return boundedObjectItems(body, changemarkers.MaxEvents+1) }
	err := q.get(ctx, "/api/v1/namespaces/"+namespace+"/events?limit=257", &page)
	if ctx.Err() != nil {
		return nil, changemarkers.Unavailable, false, ctx.Err()
	}
	if err != nil {
		if markerDenied(err) {
			return nil, changemarkers.Denied, false, nil
		}
		if errors.Is(historyReadError(err), memoryhistory.ErrInvalid) {
			return nil, changemarkers.Unavailable, false, err
		}
		return nil, changemarkers.Unavailable, false, nil
	}
	if page.Kind != "EventList" || page.APIVersion != "v1" || len(page.Items) > changemarkers.MaxEvents+1 {
		return nil, changemarkers.Unavailable, false, memoryhistory.ErrInvalid
	}
	truncated := page.Metadata.Continue != "" || len(page.Items) > changemarkers.MaxEvents
	items := page.Items[:min(len(page.Items), changemarkers.MaxEvents)]
	var result []changemarkers.Marker
	seen := map[string]bool{}
	for _, event := range items {
		key := objectKey(event.Regarding)
		owners, visible := q.visible[key]
		if !visible {
			continue
		}
		object := q.objects[key]
		if event.Metadata.Namespace != namespace || event.Regarding != object.ref || event.Metadata.UID == "" || seen[event.Metadata.UID] {
			return nil, changemarkers.Unavailable, false, memoryhistory.ErrInvalid
		}
		seen[event.Metadata.UID] = true
		kind, state := markerEventKind(event.Reason, event.Regarding.Kind)
		if kind == "" {
			continue
		}
		at, until, count, ok := eventTime(event)
		if !ok {
			continue
		} // Unknown event time cannot become a precise marker.
		m := changemarkers.Marker{Kind: kind, Clock: changemarkers.KubernetesEvent, SourceUID: event.Metadata.UID, Subject: event.Regarding, Owners: owners, At: at, Until: until, Count: count, Uncertain: true, ResizeState: state}
		if m.Validate() != nil {
			return nil, changemarkers.Unavailable, false, memoryhistory.ErrInvalid
		}
		result = append(result, m)
	}
	coverage := changemarkers.Missing
	if len(result) > 0 {
		coverage = changemarkers.Partial
	}
	return result, coverage, truncated, nil
}

func eventTime(e markerEvent) (time.Time, time.Time, uint64, bool) {
	if e.Count < 0 {
		return time.Time{}, time.Time{}, 0, false
	}
	start := e.EventTime.Time.UTC()
	if start.IsZero() {
		// A legacy aggregate without a count cannot establish its cardinality.
		if e.Count == 0 && e.Series == nil {
			return time.Time{}, time.Time{}, 0, false
		}
		start = e.FirstTimestamp.Time.UTC()
	}
	if start.IsZero() {
		return time.Time{}, time.Time{}, 0, false
	}
	end, count := start, int32(1)
	if e.Series != nil {
		end, count = e.Series.LastObservedTime.Time.UTC(), e.Series.Count
	} else if e.Count > 0 {
		count = e.Count
		if !e.LastTimestamp.IsZero() {
			end = e.LastTimestamp.Time.UTC()
		}
	}
	if end.IsZero() || end.Before(start) || count <= 0 {
		return time.Time{}, time.Time{}, 0, false
	}
	return start, end, uint64(count), true
}

func markerEventKind(reason, kind string) (changemarkers.Kind, string) {
	if kind == "Pod" {
		switch reason {
		case "Scheduled":
			return changemarkers.Scheduled, ""
		case "Started":
			return changemarkers.Started, ""
		case "Killing":
			return changemarkers.Stopped, ""
		case "BackOff":
			return changemarkers.BackOff, ""
		case "Evicted":
			return changemarkers.Evicted, ""
		case "ResizeStarted":
			return changemarkers.Resize, "started"
		case "ResizeCompleted":
			return changemarkers.Resize, "completed"
		case "ResizeDeferred":
			return changemarkers.Resize, "deferred"
		case "ResizeInfeasible":
			return changemarkers.Resize, "infeasible"
		case "ResizeError":
			return changemarkers.Resize, "error"
		}
		return "", ""
	}
	switch reason {
	case "ScalingReplicaSet", "SuccessfulCreate", "SuccessfulDelete", "SuccessfulRescale":
		return changemarkers.Rollout, ""
	}
	return "", ""
}
