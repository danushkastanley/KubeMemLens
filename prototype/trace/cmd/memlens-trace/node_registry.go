package main

import (
	json "encoding/json/v2"
	"errors"

	"github.com/danushkastanley/kube-memlens/prototype/trace/admissionkube"
	"github.com/danushkastanley/kube-memlens/prototype/trace/nodebinding"
	"github.com/danushkastanley/kube-memlens/prototype/trace/nodeprofile"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
)

type endpointConfig struct {
	NodeUID           string               `json:"nodeUID"`
	NodeName          string               `json:"nodeName"`
	URL               string               `json:"url"`
	CAFile            string               `json:"caFile"`
	CertificateSHA256 string               `json:"certificateSHA256"`
	Profile           *nodeprofile.Profile `json:"profile,omitempty"`
}

type nodeRegistry struct {
	endpoints []nodebinding.Endpoint
	profiles  []nodeprofile.Profile
}

func loadRegistry(path string) (nodeRegistry, error) {
	data, err := readInstallationFile(path, 64<<10)
	if err != nil {
		return nodeRegistry{}, err
	}
	var configs []endpointConfig
	if json.Unmarshal(data, &configs, json.RejectUnknownMembers(true)) != nil || len(configs) == 0 || len(configs) > 64 {
		return nodeRegistry{}, errors.New("invalid node registry")
	}
	var registry nodeRegistry
	for _, config := range configs {
		peer, err := loadPeer(config.CAFile, config.CertificateSHA256)
		if err != nil {
			return nodeRegistry{}, err
		}
		registry.endpoints = append(registry.endpoints, nodebinding.Endpoint{NodeUID: config.NodeUID, NodeName: config.NodeName, URL: config.URL, Peer: peer})
		if config.Profile != nil {
			if config.Profile.NodeUID != config.NodeUID || config.Profile.NodeName != config.NodeName {
				return nodeRegistry{}, errors.New("node profile and endpoint identity differ")
			}
			registry.profiles = append(registry.profiles, *config.Profile)
		}
	}
	if len(registry.profiles) != 0 && len(registry.profiles) != len(registry.endpoints) {
		return nodeRegistry{}, errors.New("node registry contains mixed profile policies")
	}
	return registry, nil
}

func (r nodeRegistry) resolver(core coreclient.CoreV1Interface, mode string) (*admissionkube.Resolver, error) {
	switch mode {
	case "baseline":
		if len(r.profiles) != 0 {
			return nil, errors.New("baseline mode cannot ignore pinned node profiles")
		}
		return admissionkube.NewResolver(core), nil
	case "pinned":
		return admissionkube.NewPinnedResolver(core, r.profiles)
	default:
		return nil, errors.New("unsupported node profile mode")
	}
}
