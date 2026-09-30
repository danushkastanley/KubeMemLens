package releasebundle

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
	"github.com/danushkastanley/kube-memlens/prototype/trace/workerinstall"
)

type Engine struct {
	files  FileSet
	digest string
}

func (Engine) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "[release engine inventory]") }
func (e Engine) Digest() string           { return e.digest }

// InspectEngine checks content integrity within an authenticated release bundle.
// The bundled engine key authenticates relationships, not execution acceptance.
// No Policy or approved Programme is constructed from these bundle declarations.
func InspectEngine(source io.ReaderAt, size int64, archiveSHA256 string) (Engine, error) {
	if source == nil || size <= 0 || size > 512<<20 || !shaPattern.MatchString(archiveSHA256) {
		return Engine{}, ErrArchive
	}
	keep := map[string]int64{
		"verification/engine-release.json": 4096, "verification/engine-release.sig": 64,
		"verification/engine-public-key.bin": 32, "sources/sdk-policy.patch": 65536,
	}
	inventory, err := ReadArchive(io.NewSectionReader(source, 0, size), Tar, Limits{512 << 20, 512 << 20, 128 << 20, 10000}, keep)
	if err != nil || inventory.InputDigest() != archiveSHA256 || inventory.InputBytes() != size {
		return Engine{}, ErrArchive
	}
	for _, name := range inventory.Names() {
		if !enginePath(name) {
			return Engine{}, ErrArchive
		}
	}
	read := func(name string) []byte { entry, _ := inventory.Entry(name); data, _ := entry.Bytes(); return data }
	raw, key, signature, patch := read("verification/engine-release.json"), read("verification/engine-public-key.bin"), read("verification/engine-release.sig"), read("sources/sdk-policy.patch")
	release, err := engineRelease(raw, key, signature, patch)
	if err != nil {
		return Engine{}, ErrArchive
	}
	for _, architecture := range []string{"amd64", "arm64"} {
		for _, binary := range []string{"memlens-trace", "memlens-filecache-worker"} {
			name := "linux-" + architecture + "/" + binary
			entry, found := inventory.Entry(name)
			if !found || entry.Type != Regular || entry.Mode&0111 == 0 || (binary == "memlens-filecache-worker" && entry.SHA256 != release.Workers[architecture]) {
				return Engine{}, ErrArchive
			}
			data, err := entryData(source, entry, 128<<20)
			if err != nil || inspectExecutable(data, architecture, binary) != nil {
				return Engine{}, ErrArchive
			}
		}
	}
	return Engine{files: inventory.files, digest: "sha256:" + sum(raw)}, nil
}
func engineRelease(raw, key, signature, patch []byte) (workerinstall.EngineRelease, error) {
	var release workerinstall.EngineRelease
	if len(key) != ed25519.PublicKeySize || len(signature) != ed25519.SignatureSize || !ed25519.Verify(key, raw, signature) || decodeOCI(raw, &release) != nil {
		return release, ErrArchive
	}
	canonical, err := json.Marshal(release)
	if err != nil || !bytes.Equal(raw, canonical) || release.SourceCommit != filecache.EngineSourceCommit || release.PatchSHA256 != filecache.EnginePatchSHA256 || sum(patch) != release.PatchSHA256 || len(release.Workers) != 2 || !shaPattern.MatchString(release.Workers["amd64"]) || !shaPattern.MatchString(release.Workers["arm64"]) {
		return release, ErrArchive
	}
	return release, nil
}
func sum(data []byte) string { value := sha256.Sum256(data); return hex.EncodeToString(value[:]) }
func entryData(source io.ReaderAt, entry Entry, maximum int64) ([]byte, error) {
	if entry.Type != Regular || entry.Size <= 0 || entry.Size > maximum {
		return nil, ErrArchive
	}
	data := make([]byte, int(entry.Size))
	count, err := source.ReadAt(data, entry.offset)
	if count != len(data) || (err != nil && err != io.EOF) || sum(data) != entry.SHA256 {
		return nil, ErrArchive
	}
	return data, nil
}
func enginePath(name string) bool {
	for _, root := range []string{"linux-amd64", "linux-arm64"} {
		if name == root || name == root+"/memlens-trace" || name == root+"/memlens-filecache-worker" {
			return true
		}
	}
	for _, root := range []string{"licences", "sources"} {
		if name == root || strings.HasPrefix(name, root+"/") {
			return true
		}
	}
	if name == "verification" || name == "reference" {
		return true
	}
	switch name {
	case "verification/engine-release.json", "verification/engine-release.sig", "verification/engine-public-key.bin", "verification/filecache-node.json",
		"reference/engine-index.json", "reference/top-file-index.json", "reference/trace-open-index.json", "reference/trace-oomkill-index.json":
		return true
	default:
		return false
	}
}

func (e Engine) Files() FileSet { return e.files }
