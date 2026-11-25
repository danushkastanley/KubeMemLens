package extension

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeCertificate(t *testing.T, path string, notAfter time.Time) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "collector"}, NotBefore: notAfter.Add(-time.Hour), NotAfter: notAfter}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestServingCertificateExpiryFollowsTheMountedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tls.crt")
	first := time.Unix(1_900_000_000, 0)
	writeCertificate(t, path, first)
	if got := servingCertificateNotAfter(path); got == nil || *got != first.Unix() {
		t.Fatalf("expiry = %v, want %d", got, first.Unix())
	}
	// A rotation replaces the file; the next read must report the new expiry.
	rotated := first.Add(365 * 24 * time.Hour)
	writeCertificate(t, path, rotated)
	if got := servingCertificateNotAfter(path); got == nil || *got != rotated.Unix() {
		t.Fatalf("expiry after rotation = %v, want %d", got, rotated.Unix())
	}
}

func TestUnreadableServingCertificateReportsNoExpiry(t *testing.T) {
	directory := t.TempDir()
	malformed := filepath.Join(directory, "malformed.crt")
	if err := os.WriteFile(malformed, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", filepath.Join(directory, "missing.crt"), malformed} {
		if got := servingCertificateNotAfter(path); got != nil {
			t.Fatalf("path %q reported expiry %d", path, *got)
		}
	}
}
