package client

import (
	"strings"
	"testing"
)

func TestResourceMetricsRequireKubernetesAndNamespaceScope(t *testing.T) {
	for _, options := range []Options{{Mode: ConnectionModeHTTP, CollectorURL: "http://example.invalid"}, {Mode: ConnectionModeKubeProxy}, {ReadScope: AllNamespacesScope()}} {
		_, err := NewResourceMetricsSource(options)
		if err == nil || (!strings.Contains(err.Error(), "Kubernetes API") && !strings.Contains(err.Error(), "one explicit namespace")) {
			t.Fatalf("unexpected scope/transport result: %v", err)
		}
	}
}
