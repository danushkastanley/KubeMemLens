package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/incident"
	"github.com/danushkastanley/kube-memlens/internal/incidentsession"
	"github.com/danushkastanley/kube-memlens/internal/incidentsessionapi"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

type sessionNamespace struct{}

func (sessionNamespace) Lookup(context.Context, string) (string, error) { return "namespace-uid", nil }

type sessionEvidence struct{ sequence atomic.Uint64 }

func (s *sessionEvidence) CapturePod(_ context.Context, p incidentsession.Principal, name string) ([]byte, error) {
	now := time.Now().UTC()
	return json.Marshal(api.IncidentBundle{SchemaVersion: 1, CapturedAt: now, ToolVersion: "fixture", Pods: []api.PodSnapshot{{Namespace: p.Namespace, PodName: name, PodUID: "pod-uid", CapturedAt: now, Memory: model.MemoryBreakdown{TotalBytes: 128 * s.sequence.Add(1)}}}})
}
func (*sessionEvidence) CaptureMarkers(context.Context, incidentsession.Principal, string, memoryhistory.Query) ([]byte, error) {
	return nil, incidentsession.ErrDisabled
}

func sessionCommandFixture(t *testing.T) collectorOptionsProvider {
	t.Helper()
	authority, err := incidentsessionapi.NewAuthority([]string{"team-a"}, sessionNamespace{}, authorizer.AuthorizerFunc(func(context.Context, authorizer.Attributes) (authorizer.Decision, string, error) {
		return authorizer.DecisionAllow, "", nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := incidentsessionapi.NewHandler(context.Background(), authority, incidentsession.DefaultLimits(), &sessionEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(handler.Shutdown)
	factory := apirequest.RequestInfoFactory{APIPrefixes: sets.NewString("api", "apis"), GrouplessAPIPrefixes: sets.NewString("api")}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info, err := factory.NewRequestInfo(r)
		if err != nil {
			t.Error(err)
			return
		}
		ctx := apirequest.WithRequestInfo(r.Context(), info)
		ctx = apirequest.WithUser(ctx, &user.DefaultInfo{Name: "operator", Groups: []string{user.AllAuthenticated}})
		handler.ServeHTTP(w, r.WithContext(ctx))
	}))
	t.Cleanup(server.Close)
	config := clientcmdapi.NewConfig()
	config.Clusters["test"] = &clientcmdapi.Cluster{Server: server.URL, CertificateAuthorityData: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})}
	config.AuthInfos["test"] = &clientcmdapi.AuthInfo{}
	config.Contexts["test"] = &clientcmdapi.Context{Cluster: "test", AuthInfo: "test"}
	config.CurrentContext = "test"
	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := clientcmd.WriteToFile(*config, path); err != nil {
		t.Fatal(err)
	}
	return func() client.Options {
		return client.Options{Mode: client.ConnectionModeKubernetesAPI, Kubeconfig: path}
	}
}

func runSessionCommand(t *testing.T, options collectorOptionsProvider, input string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := newIncidentSessionCommand(options)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(input))
	cmd.SetArgs(append([]string{"-n", "team-a"}, args...))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	err := cmd.Execute()
	return out.String(), err
}

func TestIncidentSessionCLICollectCompareAndExport(t *testing.T) {
	options := sessionCommandFixture(t)
	output, err := runSessionCommand(t, options, "", "start", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var started incidentsession.Summary
	if json.Unmarshal([]byte(output), &started) != nil || started.ID == "" {
		t.Fatal("missing session ID")
	}
	output, err = runSessionCommand(t, options, "private decision\n", "annotate", started.ID, "--note-file", "-")
	if err != nil || strings.Contains(output, "private decision") {
		t.Fatal("annotation failed or echoed private text", err)
	}
	for range 2 {
		if _, err := runSessionCommand(t, options, "", "capture", started.ID, "app"); err != nil {
			t.Fatal(err)
		}
	}
	output, err = runSessionCommand(t, options, "", "compare", started.ID, "evidence-1", "evidence-2")
	if err != nil || !strings.Contains(output, "Retained Pod memory comparison") || !strings.Contains(output, "DELTA") {
		t.Fatal("comparison not rendered", err)
	}
	publicPath := filepath.Join(t.TempDir(), "public.json")
	if _, err := runSessionCommand(t, options, "", "export", started.ID, "-o", publicPath); err != nil {
		t.Fatal(err)
	}
	public, err := os.ReadFile(publicPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(public), "private decision") || strings.Contains(string(public), "team-a") {
		t.Fatal("default export leaked private data")
	}
	privatePath := filepath.Join(t.TempDir(), "private.json")
	if _, err := runSessionCommand(t, options, "", "export", started.ID, "--include-sensitive", "-o", privatePath); err != nil {
		t.Fatal(err)
	}
	doc, err := incident.ReadSession(privatePath)
	if err != nil || doc.Authorised == nil || len(doc.Authorised.Captures) != 2 {
		t.Fatal("full export lost captures", err)
	}
	offline := func() client.Options {
		t.Fatal("offline replay contacted connection settings")
		return client.Options{}
	}
	output, err = runSessionCommand(t, offline, "", "replay", privatePath)
	if err != nil || strings.Contains(output, "private decision") {
		t.Fatal("default offline replay disclosed private annotations", err)
	}
	output, err = runSessionCommand(t, offline, "", "replay", privatePath, "--include-sensitive")
	if err != nil || !strings.Contains(output, "private decision") {
		t.Fatal("explicit private replay lost annotations", err)
	}
	output, err = runSessionCommand(t, offline, "", "compare-export", privatePath, "evidence-1", "evidence-2")
	if err != nil || !strings.Contains(output, "Retained Pod memory comparison") {
		t.Fatal("offline comparison unavailable", err)
	}
	info, err := os.Stat(privatePath)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("private export permissions")
	}
}

func TestIncidentSessionCLIShowsGapsAndLifecycle(t *testing.T) {
	options := sessionCommandFixture(t)
	output, err := runSessionCommand(t, options, "", "start", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var started incidentsession.Summary
	if json.Unmarshal([]byte(output), &started) != nil {
		t.Fatal("start")
	}
	output, err = runSessionCommand(t, options, "", "markers", started.ID, "app")
	if err != nil || !strings.Contains(output, "Evidence gap: source disabled") {
		t.Fatal("disabled source hidden", err)
	}
	output, err = runSessionCommand(t, options, "", "show", started.ID)
	if err != nil || !strings.Contains(output, "source disabled") {
		t.Fatal("timeline gap hidden", err)
	}
	output, err = runSessionCommand(t, options, "", "close", started.ID)
	if err != nil || !strings.Contains(output, "closed") {
		t.Fatal("close failed", err)
	}
	if _, err := runSessionCommand(t, options, "", "status", started.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := runSessionCommand(t, options, "", "delete", started.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := runSessionCommand(t, options, "", "status", started.ID); err == nil {
		t.Fatal("deleted session still available")
	}
}
