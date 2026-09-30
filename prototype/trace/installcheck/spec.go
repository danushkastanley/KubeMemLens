// Package installcheck validates an installation before trace workloads start.
// It performs no BPF operations and never grants runtime admission authority.
package installcheck

import (
	json "encoding/json/v2"
	"errors"
	"regexp"
	"strings"

	"github.com/danushkastanley/kube-memlens/prototype/trace/nodeprofile"
	"k8s.io/apimachinery/pkg/util/validation"
)

const MaxSpecBytes = 64 << 10

var (
	ErrConfiguration = errors.New("trace installation configuration is invalid")
	ErrUnavailable   = errors.New("trace installation prerequisites are unavailable")
	ErrMismatch      = errors.New("trace installation does not match the selected node profile")
	ErrTrust         = errors.New("trace installation certificate or policy verification failed")
)

type Node struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	UID               string `json:"uid"`
	Architecture      string `json:"architecture"`
	KernelVersion     string `json:"kernelVersion"`
	RuntimeVersion    string `json:"runtimeVersion"`
	TLSSecret         string `json:"tlsSecret"`
	CertificateSHA256 string `json:"certificateSHA256"`
	KubeletCgroupRoot string `json:"kubeletCgroupRoot"`
}

type Spec struct {
	SchemaVersion            int    `json:"schemaVersion"`
	Namespace                string `json:"namespace"`
	APIServiceName           string `json:"apiServiceName"`
	NodeServicePrefix        string `json:"nodeServicePrefix"`
	PolicySHA256             string `json:"policySHA256"`
	ControlCertificateSHA256 string `json:"controlCertificateSHA256"`
	APICABundle              []byte `json:"apiCABundle"`
	Nodes                    []Node `json:"nodes"`
}

func Decode(data []byte) (Spec, error) {
	var spec Spec
	if len(data) == 0 || len(data) > MaxSpecBytes || json.Unmarshal(data, &spec, json.RejectUnknownMembers(true)) != nil || spec.Validate() != nil {
		return Spec{}, ErrConfiguration
	}
	return spec, nil
}

var cgroupPattern = regexp.MustCompile(`^/([a-z0-9]+(-[a-z0-9]+)*(/[a-z0-9]+(-[a-z0-9]+)*){0,3})?$`)

func (n Node) profile() nodeprofile.Profile {
	return nodeprofile.Profile{NodeName: n.Name, NodeUID: n.UID, Architecture: n.Architecture, KernelVersion: n.KernelVersion, RuntimeVersion: n.RuntimeVersion}
}

func (s Spec) Validate() error {
	if s.SchemaVersion != 1 || len(validation.IsDNS1123Label(s.Namespace)) != 0 || len(validation.IsDNS1123Label(s.APIServiceName)) != 0 || len(s.NodeServicePrefix) > 42 || len(validation.IsDNS1123Label(s.NodeServicePrefix)) != 0 || !validSHA(s.PolicySHA256) || !validSHA(s.ControlCertificateSHA256) || len(s.APICABundle) == 0 || len(s.APICABundle) > 64<<10 || len(s.Nodes) == 0 || len(s.Nodes) > 64 {
		return ErrConfiguration
	}
	names, uids, ids := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, n := range s.Nodes {
		if len(n.ID) > 20 || len(validation.IsDNS1123Label(n.ID)) != 0 || n.profile().Validate() != nil ||
			len(validation.IsDNS1123Label(n.TLSSecret)) != 0 || !validSHA(n.CertificateSHA256) ||
			len(n.KubeletCgroupRoot) > 256 || !cgroupPattern.MatchString(n.KubeletCgroupRoot) ||
			names[n.Name] || uids[n.UID] || ids[n.ID] {
			return ErrConfiguration
		}
		for _, part := range strings.Split(n.KubeletCgroupRoot, "/") {
			if len(part) > 63 {
				return ErrConfiguration
			}
		}
		names[n.Name], uids[n.UID], ids[n.ID] = true, true, true
	}
	return nil
}

func validSHA(value string) bool {
	return len(value) == 64 && strings.Trim(value, "0123456789abcdef") == ""
}
