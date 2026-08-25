package certbootstrap

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
)

func TestGenerateCreatesVerifiableServiceCertificate(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	opts := Options{
		Namespace: "kube-memlens", ServiceName: "kube-memlens-collector", Validity: 24 * time.Hour,
	}
	caPEM, certPEM, keyPEM, err := generate(opts, now)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		t.Fatalf("serving key pair: %v", err)
	}
	caBlock, _ := pem.Decode(caPEM)
	certBlock, _ := pem.Decode(certPEM)
	if caBlock == nil || certBlock == nil {
		t.Fatal("generated PEM is incomplete")
	}
	ca, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		t.Fatalf("parse CA: %v", err)
	}
	certificate, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		t.Fatalf("parse serving certificate: %v", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	if _, err := certificate.Verify(x509.VerifyOptions{
		Roots: roots, DNSName: "kube-memlens-collector.kube-memlens.svc", CurrentTime: now,
	}); err != nil {
		t.Fatalf("verify serving certificate: %v", err)
	}
}

func TestNeedsRotationRejectsBrokenOrExpiringMaterial(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	opts := Options{Namespace: "kube-memlens", ServiceName: "collector", Validity: 24 * time.Hour}
	ca, cert, key, err := generate(opts, now)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	secret := &corev1.Secret{Data: map[string][]byte{"ca.crt": ca, corev1.TLSCertKey: cert, corev1.TLSPrivateKeyKey: key}}
	opts.RotateBefore = time.Hour
	if needsRotation(secret, opts, now) {
		t.Fatal("fresh certificate requires rotation")
	}
	if !needsRotation(secret, opts, now.Add(23*time.Hour+time.Minute)) {
		t.Fatal("expiring certificate did not require rotation")
	}
	secret.Data[corev1.TLSPrivateKeyKey] = []byte("not a key")
	if !needsRotation(secret, opts, now) {
		t.Fatal("broken key pair did not require rotation")
	}
}

func TestNeedsRotationRejectsUnusableTrustMaterial(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	opts := Options{
		Namespace: "kube-memlens", ServiceName: "kube-memlens-collector",
		Validity: 24 * time.Hour, RotateBefore: time.Hour,
	}
	ca, cert, key, err := generate(opts, now)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	valid := map[string][]byte{"ca.crt": ca, corev1.TLSCertKey: cert, corev1.TLSPrivateKeyKey: key}
	clone := func() *corev1.Secret {
		data := map[string][]byte{}
		for name, value := range valid {
			data[name] = append([]byte{}, value...)
		}
		return &corev1.Secret{Data: data}
	}

	invalidCA := clone()
	invalidCA.Data["ca.crt"] = []byte("not a CA")
	if !needsRotation(invalidCA, opts, now) {
		t.Fatal("invalid CA did not require rotation")
	}

	otherCA, _, _, err := generate(opts, now)
	if err != nil {
		t.Fatalf("generate other CA: %v", err)
	}
	mismatchedCA := clone()
	mismatchedCA.Data["ca.crt"] = otherCA
	if !needsRotation(mismatchedCA, opts, now) {
		t.Fatal("mismatched CA did not require rotation")
	}

	wrongService := opts
	wrongService.ServiceName = "other-service"
	wrongCA, wrongCert, wrongKey, err := generate(wrongService, now)
	if err != nil {
		t.Fatalf("generate wrong service certificate: %v", err)
	}
	wrongDNS := &corev1.Secret{Data: map[string][]byte{
		"ca.crt": wrongCA, corev1.TLSCertKey: wrongCert, corev1.TLSPrivateKeyKey: wrongKey,
	}}
	if !needsRotation(wrongDNS, opts, now) {
		t.Fatal("wrong service DNS identity did not require rotation")
	}

	if !needsRotation(clone(), opts, now.Add(-10*time.Minute)) {
		t.Fatal("not-yet-valid certificate did not require rotation")
	}
}
