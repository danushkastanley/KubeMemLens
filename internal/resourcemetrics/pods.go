package resourcemetrics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

type podList struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Continue string `json:"continue"`
	} `json:"metadata"`
	Items []podMetric `json:"items"`
}

type podMetric struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
		UID       string `json:"uid"`
	} `json:"metadata"`
	Timestamp  metav1.Time     `json:"timestamp"`
	Window     metav1.Duration `json:"window"`
	Containers []struct {
		Name  string                     `json:"name"`
		Usage map[string]json.RawMessage `json:"usage"`
	} `json:"containers"`
}

func (s *kubernetesSource) readPods(ctx context.Context, version string) (Report, error) {
	report := Report{Availability: Available, APIVersion: group + "/" + version, Observations: []Observation{}}
	seenPods, seenCursors := map[string]bool{}, map[string]bool{}
	cursor := ""
	containerCount := 0
	now := s.opts.Now().UTC()
	for page := 0; page < s.opts.MaxPages; page++ {
		query := url.Values{"limit": {strconv.Itoa(s.opts.PageSize)}}
		if cursor != "" {
			query.Set("continue", cursor)
		}
		var list podList
		status, err := s.get(ctx, "/apis/"+report.APIVersion+"/namespaces/"+s.opts.Namespace+"/pods", query, &list)
		if err != nil || status != http.StatusOK {
			failure := failureReport(status, err, false)
			failure.APIVersion = report.APIVersion
			return failure, err
		}
		if list.APIVersion != report.APIVersion || list.Kind != "PodMetricsList" {
			return Report{Availability: Unavailable, Reason: InvalidResponse}, nil
		}
		for _, pod := range list.Items {
			if pod.Metadata.Namespace != s.opts.Namespace || len(validation.IsDNS1123Subdomain(pod.Metadata.Name)) != 0 || len(pod.Metadata.UID) > 128 || seenPods[pod.Metadata.Name] {
				return Report{Availability: Unavailable, Reason: InvalidResponse}, nil
			}
			if len(seenPods) >= s.opts.MaxPods {
				return limitedReport(report), nil
			}
			seenPods[pod.Metadata.Name] = true
			if len(pod.Containers) == 0 {
				report.Availability, report.Reason = Partial, IncompleteUsage
			}
			seenContainers := map[string]bool{}
			for _, container := range pod.Containers {
				if containerCount >= s.opts.MaxContainers {
					return limitedReport(report), nil
				}
				containerCount++
				if len(validation.IsDNS1123Label(container.Name)) != 0 || seenContainers[container.Name] {
					return Report{Availability: Unavailable, Reason: InvalidResponse}, nil
				}
				seenContainers[container.Name] = true
				memory, memoryOK := wireUsageValue(container.Usage["memory"], 0)
				cpu, cpuOK := wireUsageValue(container.Usage["cpu"], 9)
				if !memoryOK || !cpuOK || pod.Timestamp.IsZero() || pod.Window.Duration <= 0 {
					report.OmittedContainers++
					report.Availability, report.Reason = Partial, IncompleteUsage
					continue
				}
				freshness := Fresh
				timestamp := pod.Timestamp.Time.UTC()
				switch {
				case timestamp.Sub(now) > s.opts.MaxFutureSkew:
					freshness = Future
				case now.Sub(timestamp) > s.opts.MaxAge:
					freshness = Old
				}
				report.Observations = append(report.Observations, Observation{Identity: Identity{Namespace: s.opts.Namespace, PodName: pod.Metadata.Name, PodUID: pod.Metadata.UID, ContainerName: container.Name}, APIVersion: report.APIVersion, Timestamp: timestamp, Window: pod.Window.Duration, Freshness: freshness, CPUUsageNanocores: cpu, MemoryWorkingSetBytes: memory})
			}
		}
		cursor = list.Metadata.Continue
		if cursor == "" {
			return finishReport(report), nil
		}
		if len(cursor) > 4096 || seenCursors[cursor] {
			return Report{Availability: Unavailable, Reason: InvalidResponse}, nil
		}
		seenCursors[cursor] = true
	}
	return limitedReport(report), nil
}

func limitedReport(report Report) Report {
	report.Availability, report.Reason = Partial, LimitReached
	return finishReport(report)
}

func finishReport(report Report) Report {
	sort.Slice(report.Observations, func(i, j int) bool {
		a, b := report.Observations[i].Identity, report.Observations[j].Identity
		if a.PodName != b.PodName {
			return a.PodName < b.PodName
		}
		return a.ContainerName < b.ContainerName
	})
	if report.Availability != Available || len(report.Observations) == 0 {
		return report
	}
	stale := 0
	for _, item := range report.Observations {
		if item.Freshness != Fresh {
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
