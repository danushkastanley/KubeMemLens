package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/url"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/rest"
)

// This binding comes from the separately verified disposable EKS campaign.
// It is not discovery, an AWS credential, or permission to create a cluster.
type eksEndpointBinding struct {
	Endpoint string `json:"endpoint"`
	CASHA256 string `json:"caSHA256"`
}

func (b *eksEndpointBinding) validate() error {
	endpoint, err := url.Parse(b.Endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.User != nil || endpoint.Path != "" ||
		endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Opaque != "" || strings.ContainsAny(b.Endpoint, "?#") ||
		(endpoint.Port() != "" && endpoint.Port() != "443") {
		return errQualification
	}
	host := strings.ToLower(endpoint.Hostname())
	if !(strings.HasSuffix(host, ".eks.amazonaws.com") || strings.HasSuffix(host, ".api.aws")) ||
		len(b.Endpoint) > 512 || len(validation.IsDNS1123Subdomain(host)) != 0 || len(b.CASHA256) != 64 || strings.Trim(b.CASHA256, "0123456789abcdef") != "" {
		return errQualification
	}
	return nil
}

func (c configuration) validContext() bool {
	if c.EKS == nil {
		return strings.HasPrefix(c.Context, "kind-")
	}
	return c.Context == "qualification-eks" && c.EKS.validate() == nil
}

func fixtureCredentials(c *rest.Config, binding *eksEndpointBinding) error {
	endpoint, err := url.Parse(c.Host)
	if err != nil || endpoint.Scheme != "https" || endpoint.User != nil || endpoint.RawQuery != "" ||
		endpoint.Fragment != "" || (endpoint.Path != "" && endpoint.Path != "/") {
		return errQualification
	}
	if binding == nil {
		host := endpoint.Hostname()
		if host != "localhost" && !net.ParseIP(host).IsLoopback() {
			return errQualification
		}
	} else {
		ca := sha256.Sum256(c.CAData)
		if binding.validate() != nil || c.Host != binding.Endpoint || hex.EncodeToString(ca[:]) != binding.CASHA256 {
			return errQualification
		}
	}
	// Preserve token-only tenant identity for both transports. Neither an exec
	// plugin nor an administrative certificate can replace the fixture token.
	if c.Insecure || c.ServerName != "" || len(c.CAData) == 0 || c.CAFile != "" ||
		c.BearerToken == "" || c.BearerTokenFile != "" || c.Username != "" || c.Password != "" ||
		c.Impersonate.UserName != "" || c.Impersonate.UID != "" || len(c.Impersonate.Groups) != 0 || len(c.Impersonate.Extra) != 0 ||
		len(c.CertData) != 0 || len(c.KeyData) != 0 || c.CertFile != "" || c.KeyFile != "" ||
		c.ExecProvider != nil || c.AuthProvider != nil || c.Proxy != nil || c.Transport != nil || c.WrapTransport != nil {
		return errQualification
	}
	return nil
}
