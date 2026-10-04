package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func eksConfig() (*rest.Config, *eksEndpointBinding) {
	c := localConfig()
	c.Host = "https://owned.us-east-1.eks.amazonaws.com"
	ca := sha256.Sum256(c.CAData)
	return c, &eksEndpointBinding{Endpoint: c.Host, CASHA256: hex.EncodeToString(ca[:])}
}

func TestEKSCredentialsRequireExactEndpointAndCA(t *testing.T) {
	c, b := eksConfig()
	if fixtureCredentials(c, b) != nil {
		t.Fatal("explicit endpoint/CA binding rejected")
	}
	for _, change := range []func(*rest.Config){
		func(c *rest.Config) { c.Host = "https://other.us-east-1.eks.amazonaws.com" },
		func(c *rest.Config) { c.CAData = []byte("other CA") },
		func(c *rest.Config) { c.Insecure = true },
		func(c *rest.Config) { c.ServerName = "other" },
		func(c *rest.Config) { c.ExecProvider = &clientcmdapi.ExecConfig{Command: "credential-helper"} },
		func(c *rest.Config) { c.CertData = []byte("admin certificate") },
		func(c *rest.Config) { c.Impersonate.UserName = "admin" },
		func(c *rest.Config) { c.BearerTokenFile = "alternate-token" },
	} {
		c, b := eksConfig()
		change(c)
		if fixtureCredentials(c, b) == nil {
			t.Fatal("changed cluster or alternate credentials accepted")
		}
	}
	if fixtureCredentials(c, nil) == nil {
		t.Fatal("EKS endpoint accepted without its explicit binding")
	}
}

func TestEKSEndpointRejectsAmbiguityAndUnrelatedHosts(t *testing.T) {
	for _, endpoint := range []string{"https://localhost", "https://example.com", "https://127.0.0.1",
		"http://owned.eks.amazonaws.com", "https://user@owned.eks.amazonaws.com", "https://owned.eks.amazonaws.com/",
		"https://owned.eks.amazonaws.com?", "https://owned.eks.amazonaws.com#", "https://.eks.amazonaws.com",
		"https://owned.eks.amazonaws.com/path", "https://owned.eks.amazonaws.com?q=1", "https://owned.eks.amazonaws.com#f",
		"https://owned.eks.amazonaws.com:6443", "https://owned.eks.amazonaws.com.attacker.invalid"} {
		_, binding := eksConfig()
		binding.Endpoint = endpoint
		if binding.validate() == nil {
			t.Fatal("ambiguous or unapproved provider endpoint accepted")
		}
	}
	_, binding := eksConfig()
	binding.Endpoint = "https://owned.eks.us-east-1.api.aws"
	if binding.validate() != nil {
		t.Fatal("explicit dual-stack endpoint rejected")
	}
	binding.CASHA256 = strings.Repeat("A", 64)
	if binding.validate() == nil {
		t.Fatal("non-canonical CA pin accepted")
	}
}

func TestEKSConfigurationRequiresDedicatedContextAndStrictBinding(t *testing.T) {
	_, binding := eksConfig()
	doc := fixtureDocument()
	doc["context"], doc["eks"] = "qualification-eks", binding
	data, _ := json.Marshal(doc)
	if _, err := readConfiguration(writeFixture(t, data)); err != nil {
		t.Fatal("bound provider fixture rejected", err)
	}
	for _, context := range []string{"production", "kind-owned", ""} {
		doc["context"] = context
		data, _ = json.Marshal(doc)
		if _, err := readConfiguration(writeFixture(t, data)); err == nil {
			t.Fatal("provider fixture accepted another context")
		}
	}
	doc["context"] = "qualification-eks"
	doc["eks"] = map[string]string{"endpoint": binding.Endpoint, "caSHA256": binding.CASHA256, "allowInsecure": "true"}
	data, _ = json.Marshal(doc)
	if _, err := readConfiguration(writeFixture(t, data)); err == nil {
		t.Fatal("unknown provider binding field accepted")
	}
	delete(doc, "eks")
	data, _ = json.Marshal(doc)
	if _, err := readConfiguration(writeFixture(t, data)); err == nil {
		t.Fatal("provider context accepted without explicit binding")
	}
}
