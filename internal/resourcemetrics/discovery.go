package resourcemetrics

import (
	"context"
	"net/http"
	"slices"
)

// Discover checks the served API contract without reading workload metrics.
// Available means the API is advertised, not that this caller can list it.
func (s *kubernetesSource) Discover(ctx context.Context) (Report, error) {
	ctx, cancel := context.WithTimeout(ctx, s.opts.Timeout)
	defer cancel()
	version, failure, err := s.discover(ctx)
	if failure != nil {
		return *failure, err
	}
	return Report{Availability: Available, APIVersion: group + "/" + version}, nil
}

type groupDiscovery struct {
	Name     string `json:"name"`
	Versions []struct {
		Version      string `json:"version"`
		GroupVersion string `json:"groupVersion"`
	} `json:"versions"`
}

type resourceDiscovery struct {
	GroupVersion string `json:"groupVersion"`
	Resources    []struct {
		Name       string   `json:"name"`
		Namespaced bool     `json:"namespaced"`
		Verbs      []string `json:"verbs"`
	} `json:"resources"`
}

func (s *kubernetesSource) discover(ctx context.Context) (string, *Report, error) {
	var advertised groupDiscovery
	status, err := s.get(ctx, "/apis/"+group, nil, &advertised)
	if err != nil || status != http.StatusOK {
		report := failureReport(status, err, true)
		return "", &report, err
	}
	if advertised.Name != group {
		report := Report{Availability: Unavailable, Reason: InvalidResponse}
		return "", &report, nil
	}
	version := ""
	for _, candidate := range []string{"v1", "v1beta1"} {
		for _, served := range advertised.Versions {
			if served.Version == candidate && served.GroupVersion == group+"/"+candidate {
				version = candidate
				break
			}
		}
		if version != "" {
			break
		}
	}
	if version == "" {
		report := Report{Availability: Unavailable, Reason: UnsupportedAPI}
		return "", &report, nil
	}
	var resources resourceDiscovery
	status, err = s.get(ctx, "/apis/"+group+"/"+version, nil, &resources)
	if err != nil || status != http.StatusOK {
		report := failureReport(status, err, false)
		return "", &report, err
	}
	if resources.GroupVersion != group+"/"+version {
		report := Report{Availability: Unavailable, Reason: InvalidResponse}
		return "", &report, nil
	}
	for _, resource := range resources.Resources {
		if resource.Name == "pods" && resource.Namespaced && slices.Contains(resource.Verbs, "list") {
			return version, nil, nil
		}
	}
	report := Report{Availability: Unavailable, Reason: UnsupportedAPI}
	return "", &report, nil
}
