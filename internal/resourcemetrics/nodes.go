package resourcemetrics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

type NodeObservation struct {
	NodeName              string        `json:"nodeName"`
	NodeUID               string        `json:"nodeUID,omitempty"`
	APIVersion            string        `json:"apiVersion"`
	Timestamp             time.Time     `json:"timestamp"`
	Window                time.Duration `json:"windowNanoseconds"`
	Freshness             Freshness     `json:"freshness"`
	CPUUsageNanocores     uint64        `json:"cpuUsageNanocores"`
	CPUUsageKnown         bool          `json:"cpuUsageKnown"`
	MemoryWorkingSetBytes uint64        `json:"memoryWorkingSetBytes"`
}

type NodeReport struct {
	Availability Availability      `json:"availability"`
	Reason       Reason            `json:"reason,omitempty"`
	APIVersion   string            `json:"apiVersion,omitempty"`
	Observations []NodeObservation `json:"observations"`
	OmittedNodes int               `json:"omittedNodes"`
}

type nodeMetric struct {
	APIVersion string                                `json:"apiVersion"`
	Kind       string                                `json:"kind"`
	Metadata   struct{ Name, Namespace, UID string } `json:"metadata"`
	Timestamp  metav1.Time                           `json:"timestamp"`
	Window     metav1.Duration                       `json:"window"`
	Usage      map[string]json.RawMessage            `json:"usage"`
}

func (s *kubernetesSource) ReadNodes(ctx context.Context, names []string) (NodeReport, error) {
	if len(names) > s.opts.MaxNodes {
		return NodeReport{Availability: Partial, Reason: LimitReached, OmittedNodes: len(names)}, nil
	}
	names = slices.Clone(names)
	slices.Sort(names)
	names = slices.Compact(names)
	for _, name := range names {
		if len(validation.IsDNS1123Subdomain(name)) != 0 {
			return NodeReport{Availability: Unavailable, Reason: InvalidResponse}, nil
		}
	}
	if len(names) == 0 {
		return NodeReport{Availability: Available, Observations: []NodeObservation{}}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.opts.Timeout)
	defer cancel()
	version, failure, err := s.discoverResource(ctx, "nodes", "get")
	if failure != nil {
		return nodeFailure(*failure), err
	}
	report := NodeReport{Availability: Available, APIVersion: group + "/" + version, Observations: []NodeObservation{}}
	for _, name := range names {
		var value nodeMetric
		status, err := s.get(ctx, "/apis/"+report.APIVersion+"/nodes/"+name, nil, &value)
		if status == http.StatusNotFound && err == nil {
			report.Availability, report.Reason = Partial, IncompleteUsage
			report.OmittedNodes++
			continue
		}
		if err != nil || status != http.StatusOK {
			return nodeFailure(failureReport(status, err, false)), err
		}
		if value.Kind != "NodeMetrics" || value.APIVersion != report.APIVersion || value.Metadata.Name != name || !s.addNode(&report, value) {
			return NodeReport{Availability: Unavailable, Reason: InvalidResponse}, nil
		}
	}
	return finishNodes(report), nil
}

// ListNodes requires an explicitly cluster-scoped source. Namespaced readers
// use ReadNodes only for names already present in their authorised inventory.
func (s *kubernetesSource) ListNodes(ctx context.Context) (NodeReport, error) {
	if s.opts.Namespace != "" {
		return NodeReport{Availability: Forbidden, Reason: AccessDenied}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.opts.Timeout)
	defer cancel()
	version, failure, err := s.discoverResource(ctx, "nodes", "list")
	if failure != nil {
		return nodeFailure(*failure), err
	}
	report := NodeReport{Availability: Available, APIVersion: group + "/" + version, Observations: []NodeObservation{}}
	seen, cursors := map[string]bool{}, map[string]bool{}
	cursor := ""
	for page := 0; page < s.opts.MaxPages; page++ {
		query := url.Values{"limit": {strconv.Itoa(s.opts.PageSize)}}
		if cursor != "" {
			query.Set("continue", cursor)
		}
		var list struct {
			APIVersion string `json:"apiVersion"`
			Kind       string `json:"kind"`
			Metadata   struct {
				Continue string `json:"continue"`
			} `json:"metadata"`
			Items []nodeMetric `json:"items"`
		}
		status, err := s.get(ctx, "/apis/"+report.APIVersion+"/nodes", query, &list)
		if err != nil || status != http.StatusOK {
			return nodeFailure(failureReport(status, err, false)), err
		}
		if list.Kind != "NodeMetricsList" || list.APIVersion != report.APIVersion {
			return NodeReport{Availability: Unavailable, Reason: InvalidResponse}, nil
		}
		for _, value := range list.Items {
			if seen[value.Metadata.Name] {
				return NodeReport{Availability: Unavailable, Reason: InvalidResponse}, nil
			}
			if len(seen) >= s.opts.MaxNodes {
				report.Availability, report.Reason = Partial, LimitReached
				return finishNodes(report), nil
			}
			seen[value.Metadata.Name] = true
			if !s.addNode(&report, value) {
				return NodeReport{Availability: Unavailable, Reason: InvalidResponse}, nil
			}
		}
		cursor = list.Metadata.Continue
		if cursor == "" {
			return finishNodes(report), nil
		}
		if len(cursor) > 4096 || cursors[cursor] || len(list.Items) == 0 {
			return NodeReport{Availability: Unavailable, Reason: InvalidResponse}, nil
		}
		cursors[cursor] = true
	}
	report.Availability, report.Reason = Partial, LimitReached
	return finishNodes(report), nil
}

func (s *kubernetesSource) addNode(report *NodeReport, value nodeMetric) bool {
	if len(validation.IsDNS1123Subdomain(value.Metadata.Name)) != 0 || value.Metadata.Namespace != "" || len(value.Metadata.UID) > 128 {
		return false
	}
	memory, known := wireUsageValue(value.Usage["memory"], 0)
	if !known || value.Timestamp.IsZero() || value.Window.Duration <= 0 {
		report.Availability, report.Reason = Partial, IncompleteUsage
		report.OmittedNodes++
		return true
	}
	cpu, cpuKnown := wireUsageValue(value.Usage["cpu"], 9)
	if !cpuKnown {
		report.Availability, report.Reason = Partial, IncompleteUsage
	}
	freshness := Fresh
	timestamp := value.Timestamp.Time.UTC()
	now := s.opts.Now().UTC()
	switch {
	case timestamp.Sub(now) > s.opts.MaxFutureSkew:
		freshness = Future
	case now.Sub(timestamp) > s.opts.MaxAge:
		freshness = Old
	}
	report.Observations = append(report.Observations, NodeObservation{NodeName: value.Metadata.Name, NodeUID: value.Metadata.UID,
		APIVersion: report.APIVersion, Timestamp: timestamp, Window: value.Window.Duration, Freshness: freshness,
		MemoryWorkingSetBytes: memory, CPUUsageNanocores: cpu, CPUUsageKnown: cpuKnown})
	return true
}

func finishNodes(report NodeReport) NodeReport {
	slices.SortFunc(report.Observations, func(a, b NodeObservation) int {
		if a.NodeName < b.NodeName {
			return -1
		}
		if a.NodeName > b.NodeName {
			return 1
		}
		return 0
	})
	if report.Availability != Available || len(report.Observations) == 0 {
		return report
	}
	stale := 0
	for _, value := range report.Observations {
		if value.Freshness != Fresh {
			stale++
		}
	}
	switch {
	case stale == len(report.Observations):
		report.Availability, report.Reason = Stale, OutsideFreshnessBounds
	case stale > 0:
		report.Availability, report.Reason = Partial, OutsideFreshnessBounds
	}
	return report
}

func nodeFailure(report Report) NodeReport {
	return NodeReport{Availability: report.Availability, Reason: report.Reason, APIVersion: report.APIVersion}
}
