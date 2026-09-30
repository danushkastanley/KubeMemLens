package client

import (
	"context"
	"net/http"
	"testing"
	"time"

	"k8s.io/client-go/rest"
)

func TestIncidentProviderKeepsReaderClusterAndScope(t *testing.T) {
	_, config := incidentTLSClient(t, incidentAPI(t))
	scope, _ := NamespaceScope("team-a")
	reader, err := NewKubernetesAPIClient(config, scope, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	config.Host = "https://different.invalid"
	connection, err := reader.OpenIncidentSessions("team-a")
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.StartIncident(context.Background()); err != nil {
		t.Fatal("reader cluster was not preserved", err)
	}
	if reader.httpClient.Timeout != time.Second || reader.httpClient.CheckRedirect != nil {
		t.Fatal("session policy mutated snapshot transport")
	}
	if _, err := reader.OpenIncidentSessions("team-b"); err == nil {
		t.Fatal("reader scope expanded")
	}
	if _, err := reader.OpenIncidentSessions("../escape"); err == nil {
		t.Fatal("invalid namespace accepted")
	}
}

func TestIncidentProviderRejectsUnverifiedTransport(t *testing.T) {
	scope, _ := NamespaceScope("team-a")
	for _, config := range []*rest.Config{{Host: "http://example.invalid"}, {Host: "https://example.invalid", TLSClientConfig: rest.TLSClientConfig{Insecure: true}}} {
		reader, err := NewKubernetesAPIClient(config, scope, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := reader.OpenIncidentSessions("team-a"); err == nil {
			t.Fatal("unverified snapshot transport gained mutation capability")
		}
	}
	reader := &KubernetesAPIClient{scope: scope, httpClient: &http.Client{}}
	if _, err := reader.OpenIncidentSessions("team-a"); err == nil {
		t.Fatal("unverified reader gained mutation capability")
	}
}
