package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
)

func TestBundleReproducesAndVerifiesAllFourObjects(t *testing.T) {
	build := os.Getenv("KML_FILECACHE_OBJECTS")
	if build == "" {
		t.Skip("requires offline candidate objects")
	}
	temporary := t.TempDir()
	keyPath := filepath.Join(temporary, "signing.key")
	if err := run([]string{"keygen", "--key", keyPath}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"keygen", "--key", keyPath}); err == nil {
		t.Fatal("existing signing key overwritten")
	}
	patch := "../../worker/sdk-policy.patch"
	first, second := filepath.Join(temporary, "first"), filepath.Join(temporary, "second")
	for _, output := range []string{first, second} {
		if err := run([]string{"build", "--build", build, "--sdk-patch", patch, "--key", keyPath, "--output", output}); err != nil {
			t.Fatal(err)
		}
	}
	if err := run([]string{"build", "--build", build, "--sdk-patch", patch, "--key", keyPath, "--output", first}); err == nil {
		t.Fatal("existing bundle overwritten")
	}
	err := filepath.WalkDir(first, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(first, path)
		if err != nil {
			return err
		}
		a, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(filepath.Join(second, relative))
		if err != nil {
			return err
		}
		if !bytes.Equal(a, b) {
			t.Fatal("bundle bytes changed between builds")
		}
		if bytes.Contains(a, []byte("PRIVATE KEY")) {
			t.Fatal("bundle included signing secret")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(first, "candidate-index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var index candidateIndex
	if json.Unmarshal(data, &index) != nil || index.Status != "unapproved" || len(index.Programmes) != 4 {
		t.Fatal("candidate index misstates approval")
	}
	public, err := os.ReadFile(filepath.Join(first, "signing-public-key.bin"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range index.Programmes {
		id := filecache.ArtifactID{Kind: entry.Kind, Architecture: entry.Architecture}
		name := string(id.Kind) + "-" + id.Architecture
		manifest, err := os.ReadFile(filepath.Join(first, "programmes", name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		signature, err := os.ReadFile(filepath.Join(first, "programmes", name+".sig"))
		if err != nil {
			t.Fatal(err)
		}
		var m filecache.Manifest
		if json.Unmarshal(manifest, &m) != nil {
			t.Fatal("manifest invalid")
		}
		object, err := os.ReadFile(filepath.Join(first, "oci/blobs/sha256", m.ObjectSHA256))
		if err != nil {
			t.Fatal(err)
		}
		oci, err := os.ReadFile(filepath.Join(first, "oci/blobs/sha256", m.OCIManifestSHA256))
		if err != nil {
			t.Fatal(err)
		}
		// This is test-only acceptance. The builder writes no installed policy.
		verifier, err := filecache.NewVerifier(ed25519.PublicKey(public), map[filecache.ArtifactID]string{id: entry.ManifestSHA256})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := verifier.Verify(id, manifest, signature, object, oci); err != nil {
			t.Fatal(err)
		}
	}
}

func TestKeyPermissionsAndInputFailuresAreClosed(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "key")
	if run([]string{"keygen", "--key", keyPath}) != nil {
		t.Fatal("keygen failed")
	}
	if os.Chmod(keyPath, 0644) != nil {
		t.Fatal("chmod failed")
	}
	if _, err := readKey(keyPath); err == nil {
		t.Fatal("shared private key accepted")
	}
	if _, err := readInputs(t.TempDir(), keyPath); err == nil {
		t.Fatal("absent build inputs accepted")
	}
	if run([]string{"approve", "--key", keyPath}) == nil {
		t.Fatal("unsupported approval action accepted")
	}
}
