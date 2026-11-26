package installcheck

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestInstallationSpecificationRejectsAmbiguousInput(t *testing.T) {
	data, err := json.Marshal(specFixture())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(data); err != nil {
		t.Fatal("valid configuration rejected", err)
	}
	for name, altered := range map[string][]byte{
		"unknown-field":     append([]byte(`{"unknown":true,`), data[1:]...),
		"duplicate-field":   append([]byte(`{"schemaVersion":1,`), data[1:]...),
		"wrong-case":        bytes.Replace(data, []byte(`"schemaVersion"`), []byte(`"SchemaVersion"`), 1),
		"future-version":    bytes.Replace(data, []byte(`"schemaVersion":1`), []byte(`"schemaVersion":2`), 1),
		"trailing-document": append(append([]byte(nil), data...), []byte(`{}`)...),
		"oversized":         bytes.Repeat([]byte(" "), MaxSpecBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(altered); err != ErrConfiguration {
				t.Fatal("ambiguous configuration accepted")
			}
		})
	}
}

func TestInstallationSpecificationBoundsAndIdentity(t *testing.T) {
	for name, mutate := range map[string]func(*Spec){
		"empty-nodes":       func(s *Spec) { s.Nodes = nil },
		"duplicate-node":    func(s *Spec) { s.Nodes = append(s.Nodes, s.Nodes[0]) },
		"duplicate-uid":     func(s *Spec) { n := s.Nodes[0]; n.Name, n.ID = "other", "two"; s.Nodes = append(s.Nodes, n) },
		"path-escape":       func(s *Spec) { s.Nodes[0].KubeletCgroupRoot = "/../kubelet" },
		"long-component":    func(s *Spec) { s.Nodes[0].KubeletCgroupRoot = "/" + strings.Repeat("a", 64) },
		"wrong-runtime":     func(s *Spec) { s.Nodes[0].RuntimeVersion = "cri-o://1.0" },
		"empty-runtime":     func(s *Spec) { s.Nodes[0].RuntimeVersion = "containerd://" },
		"bad-pin":           func(s *Spec) { s.Nodes[0].CertificateSHA256 = "not-a-digest" },
		"bad-audit-key-pin": func(s *Spec) { s.AuditReferenceKeySHA256 = "" },
		"bad-policy":        func(s *Spec) { s.PolicySHA256 = "" },
		"bad-service":       func(s *Spec) { s.APIServiceName = "other/path" },
	} {
		t.Run(name, func(t *testing.T) {
			spec := specFixture()
			mutate(&spec)
			if spec.Validate() != ErrConfiguration {
				t.Fatal("invalid bounded specification accepted")
			}
		})
	}
}
