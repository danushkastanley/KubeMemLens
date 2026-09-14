package nodebinding

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/internal/tracepreflight"
	"github.com/danushkastanley/kube-memlens/prototype/trace/targetfs"
)

type certificates struct {
	ca     *x509.Certificate
	key    *ecdsa.PrivateKey
	pool   *x509.CertPool
	serial int64
}

func newCertificates(t *testing.T) *certificates {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test CA"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(raw)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	return &certificates{ca, key, pool, 1}
}
func (c *certificates) issue(t *testing.T) (tls.Certificate, Peer) {
	t.Helper()
	c.serial++
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(c.serial), NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	raw, err := x509.CreateCertificate(rand.Reader, template, c.ca, &key.PublicKey, c.key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{raw}, PrivateKey: key}, Peer{c.pool, sha256.Sum256(raw)}
}

type testHandle struct {
	mu      sync.Mutex
	target  trace.TargetIdentity
	closed  chan struct{}
	failure error
}

func (h *testHandle) Target() trace.TargetIdentity { return h.target }
func (h *testHandle) Check(context.Context) error  { h.mu.Lock(); defer h.mu.Unlock(); return h.failure }
func (h *testHandle) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	select {
	case <-h.closed:
	default:
		close(h.closed)
	}
	return nil
}
func workload() admission.Workload {
	return admission.Workload{Target: trace.TargetIdentity{Namespace: "tenant-a", PodName: "target", PodUID: "01234567-0123-4567-8901-012345678901", ContainerName: "worker", ContainerID: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", ContainerStartedAt: time.Now().UTC().Truncate(time.Second), NodeUID: "node-uid"}, NodeName: "node", QoS: "Burstable"}
}

type fixture struct {
	service *Service
	client  *Client
	server  *httptest.Server
	handles chan *testHandle
	certs   *certificates
	control tls.Certificate
	node    Peer
}

func setup(t *testing.T) *fixture {
	t.Helper()
	c := newCertificates(t)
	control, controlPeer := c.issue(t)
	node, nodePeer := c.issue(t)
	handles := make(chan *testHandle, 32)
	resolve := func(ctx context.Context, w admission.Workload) (targetfs.Handle, error) {
		target := w.Target
		target.CgroupID = 123
		h := &testHandle{target: target, closed: make(chan struct{})}
		handles <- h
		return h, nil
	}
	s, err := NewService(context.Background(), "node-uid", "node", controlPeer, resolve, func(context.Context) (string, error) { return tracepreflight.Baseline().Digest(), nil }, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	config, err := ServerTLS(node, controlPeer)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(s)
	server.TLS = config
	server.StartTLS()
	client, err := NewClient(control, []Endpoint{{"node-uid", "node", server.URL, nodePeer}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		client.Close()
		server.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := s.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return &fixture{s, client, server, handles, c, control, nodePeer}
}
