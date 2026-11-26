package filecache

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func TestSignedOCIShapeCannotBroadenArtifact(t *testing.T) {
	root := os.Getenv("KML_FILECACHE_OBJECTS")
	if root == "" {
		t.Skip("requires offline candidate objects")
	}
	object, err := os.ReadFile(filepath.Join(root, "files-arm64-1", "program.bpf.o"))
	if err != nil {
		t.Fatal(err)
	}
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := ArtifactID{trace.Files, "arm64"}
	candidate, err := BuildCandidate(id, object, strings.Repeat("b", 64), key)
	if err != nil {
		t.Fatal(err)
	}
	for name, wrong := range map[string][]byte{
		"missing-layout":     []byte(`{"schemaVersion":2}`),
		"arbitrary-url":      bytes.Replace(candidate.OCI, []byte(`"layers":[{`), []byte(`"layers":[{"urls":["https://example.invalid/program"],`), 1),
		"wrong-architecture": bytes.ReplaceAll(candidate.OCI, []byte("arm64"), []byte("amd64")),
		"wrong-layer-digest": bytes.ReplaceAll(candidate.OCI, []byte(sumSHA(object)), []byte(strings.Repeat("a", 64))),
		"wrong-kind":         bytes.ReplaceAll(candidate.OCI, []byte(`"files"`), []byte(`"cache"`)),
		"duplicate-field":    bytes.Replace(candidate.OCI, []byte(`"schemaVersion":2`), []byte(`"schemaVersion":2,"schemaVersion":2`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			var manifest Manifest
			if json.Unmarshal(candidate.Manifest, &manifest) != nil {
				t.Fatal("manifest decode failed")
			}
			manifest.OCIManifestSHA256 = sumSHA(wrong)
			encoded, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			verifier, err := NewVerifier(public, map[ArtifactID]string{id: sumSHA(encoded)})
			if err != nil {
				t.Fatal(err)
			}
			// Even a correctly signed and independently listed digest cannot bypass
			// the fixed runtime content shape by adding another retrieval mechanism.
			if _, err := verifier.Verify(id, encoded, ed25519.Sign(key, encoded), object, wrong); err == nil {
				t.Fatal("signed invalid OCI layout accepted")
			}
		})
	}
}

func TestCandidateIsReproducibleButDoesNotGrantAcceptance(t *testing.T) {
	root := os.Getenv("KML_FILECACHE_OBJECTS")
	if root == "" {
		t.Skip("requires offline candidate objects")
	}
	object, err := os.ReadFile(filepath.Join(root, "cache-arm64-1", "program.bpf.o"))
	if err != nil {
		t.Fatal(err)
	}
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := ArtifactID{trace.Cache, "arm64"}
	first, err := BuildCandidate(id, object, strings.Repeat("b", 64), key)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildCandidate(id, object, strings.Repeat("b", 64), key)
	if err != nil || !bytes.Equal(first.Manifest, second.Manifest) || !bytes.Equal(first.Signature, second.Signature) || !bytes.Equal(first.OCI, second.OCI) {
		t.Fatal("candidate build is not deterministic")
	}
	if _, err := NewVerifier(public, nil); err == nil {
		t.Fatal("candidate signing granted acceptance")
	}
	if !ed25519.Verify(public, first.Manifest, first.Signature) {
		t.Fatal("candidate signature invalid")
	}
	key[len(key)-1] ^= 1
	if _, err := BuildCandidate(id, object, strings.Repeat("b", 64), key); err == nil {
		t.Fatal("inconsistent signing key accepted")
	}
}
