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

func TestVerifierRequiresIndependentAcceptance(t *testing.T) {
	key, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewVerifier(key, nil); err == nil {
		t.Fatal("empty acceptance configuration enabled programmes")
	}
	id := ArtifactID{trace.Files, "arm64"}
	v, err := NewVerifier(key, map[ArtifactID]string{id: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Verify(id, []byte("{}"), make([]byte, 64), []byte("ELF"), []byte("{}")); err == nil {
		t.Fatal("unapproved signed identity accepted")
	}
}

func TestEnginePolicyPatchIdentity(t *testing.T) {
	patch, err := os.ReadFile("../worker/sdk-policy.patch")
	if err != nil {
		t.Fatal(err)
	}
	if sumSHA(patch) != EnginePatchSHA256 {
		t.Fatal("SDK policy changed without changing the signed engine identity")
	}
}

func TestSignedCompiledCandidate(t *testing.T) {
	root := os.Getenv("KML_FILECACHE_OBJECTS")
	if root == "" {
		t.Skip("requires offline build_filecache.py output")
	}
	object, err := os.ReadFile(filepath.Join(root, "files-arm64-1", "program.bpf.o"))
	if err != nil {
		t.Fatal(err)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := ArtifactID{trace.Files, "arm64"}
	oci, err := OCIManifest(id, object)
	if err != nil {
		t.Fatal(err)
	}
	m := Manifest{Version: 1, Kind: id.Kind, Architecture: id.Architecture, ObjectSHA256: sumSHA(object), SourceSHA256: strings.Repeat("b", 64), OCIManifestSHA256: sumSHA(oci), EngineCommit: EngineSourceCommit, BuilderDigest: BuilderDigest, EnginePatchSHA256: EnginePatchSHA256}
	manifest, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	signature := ed25519.Sign(private, manifest)
	accepted := map[ArtifactID]string{id: sumSHA(manifest)}
	v, err := NewVerifier(public, accepted)
	if err != nil {
		t.Fatal(err)
	}
	accepted[id] = strings.Repeat("f", 64)
	public[0] ^= 1
	p, err := v.Verify(id, manifest, signature, object, oci)
	if err != nil {
		t.Fatal(err)
	}
	copyOfObject := p.Object()
	copyOfObject[0] ^= 1
	if !bytes.Equal(p.Object(), object) {
		t.Fatal("verified programme was mutable through its accessor")
	}
	if _, err := v.Verify(ArtifactID{trace.Files, "amd64"}, manifest, signature, object, oci); err == nil {
		t.Fatal("architecture binding bypassed")
	}
	if _, err := v.Verify(id, manifest, signature, copyOfObject, oci); err == nil {
		t.Fatal("modified object accepted")
	}
	if _, err := v.Verify(id, manifest, signature, object, append(oci, ' ')); err == nil {
		t.Fatal("modified OCI identity accepted")
	}
	signature[0] ^= 1
	if _, err := v.Verify(id, manifest, signature, object, oci); err == nil {
		t.Fatal("bad signature accepted")
	}
	for _, malformed := range [][]byte{
		append(append([]byte(nil), manifest...), ' '),
		bytes.Replace(manifest, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1),
		bytes.Replace(manifest, []byte(`"version":1`), []byte(`"Version":1`), 1),
		bytes.Replace(manifest, []byte(EngineSourceCommit), []byte(strings.Repeat("f", 40)), 1),
	} {
		key := private.Public().(ed25519.PublicKey)
		v, err := NewVerifier(key, map[ArtifactID]string{id: sumSHA(malformed)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := v.Verify(id, malformed, ed25519.Sign(private, malformed), object, oci); err == nil {
			t.Fatal("noncanonical or wrong-engine manifest accepted")
		}
	}
}
