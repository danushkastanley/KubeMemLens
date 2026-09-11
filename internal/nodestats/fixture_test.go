package nodestats

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

var sampleNow = time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)

func summaryBody() string {
	return `{"node":{"nodeName":"node-a","startTime":"2026-09-11T11:00:00Z","memory":{"time":"2026-09-11T12:00:00Z","availableBytes":9000,"usageBytes":1000,"workingSetBytes":600,"rssBytes":500,"pageFaults":5,"majorPageFaults":1,"psi":{"some":{"total":1500,"avg10":1.2,"avg60":0,"avg300":0},"full":{"total":22,"avg10":0,"avg60":0,"avg300":0}}},"swap":{"time":"2026-09-11T12:00:00Z","swapUsageBytes":0,"swapAvailableBytes":800}},"pods":[{"podRef":{"name":"private-pod","namespace":"other-tenant"},"volume":[{"name":"private-volume","path":"/private/path"}]}]}`
}

type harness struct {
	source  *Source
	api     *httptest.Server
	kubelet *httptest.Server
	config  *rest.Config
	opts    Options
	now     time.Time
}

func newHarness(t *testing.T, handler http.HandlerFunc, mutations ...func(*corev1.Node)) *harness {
	t.Helper()
	server, ca := tlsServer(t, handler, "127.0.0.1", time.Now().Add(time.Hour))
	h := &harness{kubelet: server, now: sampleNow}
	_, portText, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portText)
	node := testNode(port)
	for _, mutate := range mutations {
		mutate(&node)
	}
	h.api = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == identityPath && r.Header.Get("Authorization") == "Bearer api-credential" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprint(w, identityBody())
			return
		}
		if r.Method != http.MethodGet || r.URL.RequestURI() != "/api/v1/nodes/node-a" || r.Header.Get("Authorization") != "Bearer api-credential" {
			t.Errorf("unexpected API request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "denied", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(node)
	}))
	t.Cleanup(h.api.Close)
	dir := t.TempDir()
	h.opts = Options{NodeName: "node-a", CAFile: filepath.Join(dir, "ca.crt"), TokenFile: filepath.Join(dir, "token"), Now: func() time.Time { return h.now }}
	if err := os.WriteFile(h.opts.CAFile, ca, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.opts.TokenFile, []byte("node-credential"), 0600); err != nil {
		t.Fatal(err)
	}
	h.config = &rest.Config{Host: h.api.URL, BearerToken: "api-credential", TLSClientConfig: rest.TLSClientConfig{CAData: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: h.api.Certificate().Raw})}}
	h.source, err = New(h.config, h.opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.source.Close)
	return h
}

func identityBody() string {
	return `{"apiVersion":"authentication.k8s.io/v1","kind":"SelfSubjectReview","status":{"userInfo":{"username":"system:serviceaccount:fixture:node-context","extra":{"authentication.kubernetes.io/node-name":["node-a"],"authentication.kubernetes.io/node-uid":["node-uid-a"],"authentication.kubernetes.io/pod-uid":["pod-uid-a"],"authentication.kubernetes.io/credential-id":["JTI=fixture"]}}}}`
}

func testNode(port int) corev1.Node {
	return corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a", UID: "node-uid-a", Annotations: map[string]string{"private-annotation": "private-value"}},
		Status: corev1.NodeStatus{
			NodeInfo:        corev1.NodeSystemInfo{OperatingSystem: "linux", KubeletVersion: "v1.37.0"},
			Addresses:       []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "127.0.0.1"}},
			DaemonEndpoints: corev1.NodeDaemonEndpoints{KubeletEndpoint: corev1.DaemonEndpoint{Port: int32(port)}},
			Capacity:        corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("8Gi"), "hugepages-2Mi": resource.MustParse("512Mi")},
			Allocatable:     corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("7Gi"), "hugepages-2Mi": resource.MustParse("256Mi")},
			Conditions:      []corev1.NodeCondition{{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionFalse}},
		}}
}

func tlsServer(t *testing.T, handler http.Handler, ip string, expires time.Time) (*httptest.Server, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "fixture"},
		NotBefore: time.Now().Add(-24 * time.Hour), NotAfter: expires,
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP(ip)}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func successHandler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.RequestURI() != "/stats/summary" || r.Header.Get("Authorization") != "Bearer node-credential" {
			t.Error("kubelet received unexpected path, verb or credential")
			http.Error(w, "denied", 403)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, summaryBody())
	}
}
