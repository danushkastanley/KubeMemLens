package installcheck

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"
)

var fixtureTime = time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)

func issuer(t *testing.T, parent *x509.Certificate, parentKey *ecdsa.PrivateKey) (*x509.Certificate, *ecdsa.PrivateKey, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "test issuer"}, NotBefore: fixtureTime.Add(-time.Minute), NotAfter: fixtureTime.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	if parent == nil {
		parent, parentKey = cert, key
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, parent, &key.PublicKey, parentKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err = x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func leaf(t *testing.T, parent *x509.Certificate, parentKey *ecdsa.PrivateKey, name string, usage x509.ExtKeyUsage) ([]byte, []byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: serial, NotBefore: fixtureTime.Add(-time.Minute), NotAfter: fixtureTime.Add(time.Hour), DNSNames: []string{name}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}}
	der, err := x509.CreateCertificate(rand.Reader, cert, parent, &key.PublicKey, parentKey)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), der
}

func trustFixture(t *testing.T, parent *x509.Certificate, key *ecdsa.PrivateKey, ca, intermediate []byte) (Spec, TrustMaterial) {
	t.Helper()
	spec := specFixture()
	spec.APICABundle = ca
	apiCert, apiKey, _ := leaf(t, parent, key, "trial-api.trace-admin.svc", x509.ExtKeyUsageServerAuth)
	controlCert, controlKey, controlDER := leaf(t, parent, key, "control", x509.ExtKeyUsageClientAuth)
	nodeCert, _, nodeDER := leaf(t, parent, key, "trial-node-one.trace-admin.svc", x509.ExtKeyUsageServerAuth)
	spec.ControlCertificateSHA256 = digest(controlDER)
	spec.Nodes[0].CertificateSHA256 = digest(nodeDER)
	return spec, TrustMaterial{APICertificate: append(apiCert, intermediate...), APIKey: apiKey,
		ControlCertificate: append(controlCert, intermediate...), ControlKey: controlKey,
		NodeCA: ca, Nodes: map[string]NodeTrust{"one": {Certificate: append(nodeCert, intermediate...), ControlCA: ca}}}
}

func TestInstallationTrustAcceptsVerifiedChains(t *testing.T) {
	root, key, ca := issuer(t, nil, nil)
	spec, material := trustFixture(t, root, key, ca, nil)
	if err := CheckTrust(spec, material, fixtureTime); err != nil {
		t.Fatal("directly issued identities rejected", err)
	}
	intermediate, intermediateKey, chain := issuer(t, root, key)
	spec, material = trustFixture(t, intermediate, intermediateKey, ca, chain)
	if err := CheckTrust(spec, material, fixtureTime); err != nil {
		t.Fatal("verified intermediate chain rejected", err)
	}
	if err := CheckTrust(spec, material, fixtureTime.Add(2*time.Hour)); err == nil {
		t.Fatal("expired trust accepted")
	}
	for _, formatted := range []string{fmt.Sprintf("%+v", material), fmt.Sprintf("%#v", material.Nodes["one"])} {
		if strings.Contains(formatted, "CERTIFICATE") || strings.Contains(formatted, "PRIVATE KEY") || strings.Contains(formatted, "BEGIN") {
			t.Fatal("diagnostic formatting exposed trust material")
		}
	}
}

func TestInstallationTrustRejectsMismatches(t *testing.T) {
	root, key, ca := issuer(t, nil, nil)
	for name, mutate := range map[string]func(*Spec, *TrustMaterial){
		"service-name": func(s *Spec, _ *TrustMaterial) { s.APIServiceName = "other" },
		"namespace":    func(s *Spec, _ *TrustMaterial) { s.Namespace = "other" },
		"api-ca":       func(s *Spec, _ *TrustMaterial) { s.APICABundle = []byte("invalid CA") },
		"key-pair":     func(_ *Spec, m *TrustMaterial) { m.APIKey = m.ControlKey },
		"missing-key":  func(_ *Spec, m *TrustMaterial) { m.ControlKey = nil },
		"control-pin":  func(s *Spec, _ *TrustMaterial) { s.ControlCertificateSHA256 = strings.Repeat("0", 64) },
		"node-pin":     func(s *Spec, _ *TrustMaterial) { s.Nodes[0].CertificateSHA256 = strings.Repeat("0", 64) },
		"node-name":    func(s *Spec, _ *TrustMaterial) { s.NodeServicePrefix = "other" },
		"node-ca":      func(_ *Spec, m *TrustMaterial) { m.NodeCA = m.APICertificate },
		"control-ca": func(_ *Spec, m *TrustMaterial) {
			n := m.Nodes["one"]
			n.ControlCA = m.APICertificate
			m.Nodes["one"] = n
		},
		"missing-node": func(_ *Spec, m *TrustMaterial) { delete(m.Nodes, "one") },
		"node-key-usage": func(s *Spec, m *TrustMaterial) {
			cert, _, der := leaf(t, root, key, "trial-node-one.trace-admin.svc", x509.ExtKeyUsageClientAuth)
			s.Nodes[0].CertificateSHA256 = digest(der)
			n := m.Nodes["one"]
			n.Certificate = cert
			m.Nodes["one"] = n
		},
	} {
		t.Run(name, func(t *testing.T) {
			spec, material := trustFixture(t, root, key, ca, nil)
			mutate(&spec, &material)
			if err := CheckTrust(spec, material, fixtureTime); err == nil {
				t.Fatal("mismatched installation trust accepted")
			}
		})
	}
}
