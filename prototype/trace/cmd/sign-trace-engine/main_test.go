package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func signingKey(t *testing.T) string {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSigningKeyMustBePrivateBoundedRegularEd25519(t *testing.T) {
	path := signingKey(t)
	if _, err := privateKey(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := privateKey(path); err == nil {
		t.Fatal("world-readable key accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := privateKey(link); err == nil {
		t.Fatal("key link accepted")
	}
	for _, raw := range [][]byte{[]byte("not a key"), make([]byte, 4097)} {
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := privateKey(path); err == nil {
			t.Fatal("invalid key accepted")
		}
	}
}

func TestMatchingDigestDoesNotPermitSigningNonWorkerBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "worker")
	data := []byte("not a Linux worker")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	digest := hex.EncodeToString(hash[:])
	output := filepath.Join(dir, "public")
	args := []string{"--key", signingKey(t), "--output", output, "--amd64", path, "--arm64", path,
		"--amd64-sha256", digest, "--arm64-sha256", digest}
	if run(args) == nil {
		t.Fatal("non-worker file was signed")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("failed validation created public artefacts")
	}
}
