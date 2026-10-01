package qualificationendpoint

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExplicitProviderScopeAndLocalDefault(t *testing.T) {
	local := []string{"https://127.0.0.1:6443", "https://[::1]:6443", "https://owned-control-plane:6443", "https://kubernetes.default.svc:443"}
	eks := []string{"https://ABC123.gr7.us-east-1.eks.amazonaws.com", "https://owned.eu-west-2.eks.amazonaws.com:443", "https://owned.us-east-1.api.aws"}
	for _, server := range local {
		if !Allowed(server, "") || !Allowed(server, Local) || Allowed(server, EKS) {
			t.Fatalf("local scope mismatch: %s", server)
		}
	}
	for _, server := range eks {
		if Allowed(server, "") || Allowed(server, Local) || !Allowed(server, EKS) {
			t.Fatalf("EKS scope mismatch: %s", server)
		}
	}
	for _, server := range append(local, eks...) {
		if Allowed(server, "automatic") {
			t.Fatal("unknown scope accepted")
		}
	}
}

func TestMalformedAndForeignEndpointsFailClosed(t *testing.T) {
	for _, server := range []string{
		"https://owned.us-" + strings.Repeat("a", 64) + "-1.eks.amazonaws.com",
		"http://owned.us-east-1.eks.amazonaws.com", "https://owned.us-east-1.eks.amazonaws.com:6443",
		"https://owned.us-east-1.eks.amazonaws.com:", "https://owned.us-east-1.eks.amazonaws.com.",
		"https://owned.us-east-1.eks.amazonaws.com.evil.invalid", "https://owned.us-east-1.eks.amazonaws.com/path",
		"https://user@owned.us-east-1.eks.amazonaws.com", "https://owned.us-east-1.eks.amazonaws.com?",
		"https://owned.us-east-1.eks.amazonaws.com#fragment", "https://owned.us-east-1.eks.amazonaws.com/#",
		"https://eks.amazonaws.com", "https://example.com:443", "https://owned..us-east-1.eks.amazonaws.com",
		"https://-owned.us-east-1.eks.amazonaws.com", "https://owned.cn-north-1.amazonwebservices.com.cn",
		"https://owned.us-gov-west-1.eks.amazonaws.com", "https://owned.us-east-1.api.aws:0443",
	} {
		if Allowed(server, EKS) {
			t.Fatalf("unapproved EKS endpoint accepted: %s", server)
		}
	}
}

func TestScopeJSONRejectsAmbiguousOrImplicitProviderSelection(t *testing.T) {
	for _, raw := range []string{`null`, `""`, `"automatic"`, `"EKS"`, `true`, `1`, `{}`} {
		var scope Scope
		if json.Unmarshal([]byte(raw), &scope) == nil {
			t.Fatalf("invalid scope accepted: %s", raw)
		}
	}
	for _, raw := range []string{`"local"`, `"eks"`} {
		var scope Scope
		if err := json.Unmarshal([]byte(raw), &scope); err != nil {
			t.Fatal(err)
		}
	}
}
