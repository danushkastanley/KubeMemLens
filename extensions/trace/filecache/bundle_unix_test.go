//go:build linux || darwin

package filecache

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func bundleFixture(t *testing.T) (string, *Verifier, ArtifactID, Candidate) {
	t.Helper()
	build := os.Getenv("KML_FILECACHE_OBJECTS")
	if build == "" {
		t.Skip("requires offline candidate objects")
	}
	object, err := os.ReadFile(filepath.Join(build, "files-arm64-1/program.bpf.o"))
	if err != nil {
		t.Fatal(err)
	}
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := ArtifactID{"files", "arm64"}
	candidate, err := BuildCandidate(id, object, strings.Repeat("b", 64), key)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifier(public, map[ArtifactID]string{id: sumSHA(candidate.Manifest)})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for name, data := range map[string][]byte{"programmes/files-arm64.json": candidate.Manifest, "programmes/files-arm64.sig": candidate.Signature,
		"oci/blobs/sha256/" + sumSHA(candidate.Object): candidate.Object, "oci/blobs/sha256/" + sumSHA(candidate.OCI): candidate.OCI} {
		path := filepath.Join(root, name)
		if os.MkdirAll(filepath.Dir(path), 0700) != nil || os.WriteFile(path, data, 0600) != nil {
			t.Fatal("fixture write failed")
		}
	}
	return root, verifier, id, candidate
}

func TestBundleUsesOnlyIndependentAcceptance(t *testing.T) {
	path, verifier, id, candidate := bundleFixture(t)
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	// Bundled policy-like files carry no authority and are never consulted.
	if os.WriteFile(filepath.Join(path, "candidate-index.json"), []byte(`{"status":"approved"}`), 0600) != nil ||
		os.WriteFile(filepath.Join(path, "signing-public-key.bin"), make([]byte, 32), 0600) != nil {
		t.Fatal("fixture write failed")
	}
	programme, err := verifier.Load(root, id)
	if err != nil || !bytes.Equal(programme.Object(), candidate.Object) {
		t.Fatal("accepted content could not load")
	}
	otherKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewVerifier(otherKey, map[ArtifactID]string{id: sumSHA(candidate.Manifest)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Load(root, id); err == nil {
		t.Fatal("bundled candidate overrode independent key")
	}
	if os.WriteFile(filepath.Join(path, "oci/blobs/sha256", sumSHA(candidate.Object)), []byte("changed"), 0600) != nil {
		t.Fatal("fixture write failed")
	}
	if _, err := verifier.Load(root, id); err == nil {
		t.Fatal("changed content accepted")
	}
	if !bytes.Equal(programme.Object(), candidate.Object) {
		t.Fatal("loaded content changed after filesystem mutation")
	}
}

func TestBundleRejectsUnsafeOrUnboundedFiles(t *testing.T) {
	for _, replacement := range []string{"fifo", "symlink", "directory", "oversized", "truncated-signature"} {
		t.Run(replacement, func(t *testing.T) {
			path, verifier, id, candidate := bundleFixture(t)
			name := filepath.Join(path, "programmes/files-arm64.json")
			if replacement == "truncated-signature" {
				name = filepath.Join(path, "programmes/files-arm64.sig")
			}
			if os.Remove(name) != nil {
				t.Fatal("fixture removal failed")
			}
			var err error
			switch replacement {
			case "fifo":
				err = syscall.Mkfifo(name, 0600)
			case "symlink":
				target := filepath.Join(t.TempDir(), "manifest")
				if os.WriteFile(target, candidate.Manifest, 0600) != nil {
					t.Fatal("fixture write failed")
				}
				err = os.Symlink(target, name)
			case "directory":
				err = os.Mkdir(name, 0700)
			case "oversized":
				err = os.WriteFile(name, make([]byte, 2049), 0600)
			case "truncated-signature":
				err = os.WriteFile(name, candidate.Signature[:63], 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			root, err := os.OpenRoot(path)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			if _, err := verifier.Load(root, id); err == nil {
				t.Fatal("unsafe bundle file accepted")
			}
		})
	}
}

func TestBundleRejectsIntermediateEscape(t *testing.T) {
	path, verifier, id, _ := bundleFixture(t)
	outside := filepath.Join(t.TempDir(), "programmes")
	if os.Rename(filepath.Join(path, "programmes"), outside) != nil || os.Symlink(outside, filepath.Join(path, "programmes")) != nil {
		t.Fatal("fixture move failed")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := verifier.Load(root, id); err == nil {
		t.Fatal("bundle followed intermediate symlink outside root")
	}
}
