package main

import (
	"encoding/json"
	"strings"
)

func verifyRegistry(raw, namespace string) {
	var entries []struct {
		NodeUID, NodeName, URL, CAFile, CertificateSHA256 string
		Profile                                           struct {
			NodeUID, NodeName, Architecture, KernelVersion, RuntimeVersion string
		}
	}
	if len(raw) > 64<<10 || json.Unmarshal([]byte(raw), &entries) != nil || len(entries) != 2 {
		fail("invalid bounded endpoint registry")
	}
	for i, entry := range entries {
		name, arch := []string{"node-one", "node-two"}[i], []string{"amd64", "arm64"}[i]
		if entry.NodeName != name || entry.NodeUID != name+"-uid" || entry.Profile.NodeName != name || entry.Profile.NodeUID != entry.NodeUID || entry.Profile.Architecture != arch || entry.Profile.KernelVersion != "6.12.0" || entry.Profile.RuntimeVersion != "containerd://2.2.0" || entry.CAFile != "/tls/node-ca.crt" || entry.CertificateSHA256 != strings.Repeat("c", 64) || !strings.HasPrefix(entry.URL, "https://") || !strings.HasSuffix(entry.URL, "."+namespace+".svc:9443") {
			fail("registry lost its exact identity, profile or certificate binding")
		}
	}
}
