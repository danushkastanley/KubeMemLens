package releasebundle

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
)

type Programmes struct {
	files   FileSet
	digest  string
	headers map[string]string
}

func (Programmes) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[release programme inventory]")
}
func (p Programmes) Digest() string { return p.digest }

// InspectProgrammes authenticates internal programme relationships after the
// enclosing release has been authenticated. It parses ELF/BTF in userspace only;
// it never creates a Verifier acceptance allowlist or an approved Programme.
func InspectProgrammes(source io.ReaderAt, size int64, archiveSHA256 string) (Programmes, error) {
	if source == nil || size <= 0 || size > 128<<20 || !shaPattern.MatchString(archiveSHA256) {
		return Programmes{}, ErrArchive
	}
	keep := map[string]int64{"candidate-index.json": 4096, "signing-public-key.bin": 32, "sdk-policy.patch": 65536, "build.json": 65536, "BUILD_COMPLETE": 64, "oci/index.json": 16384, "oci/oci-layout": 1024}
	for _, name := range []string{"LICENSE", "files.bpf.c", "cache.bpf.c", "oom.bpf.c", "common.h"} {
		keep["source/"+name] = 65536
	}
	for _, arch := range []string{"amd64", "arm64"} {
		for _, kind := range []trace.Kind{trace.Files, trace.Cache, trace.OOM} {
			name := "programmes/" + string(kind) + "-" + arch
			keep[name+".json"] = 2048
			keep[name+".sig"] = 64
		}
	}
	inventory, err := ReadArchive(io.NewSectionReader(source, 0, size), Tar, Limits{128 << 20, 128 << 20, 4 << 20, 256}, keep)
	if err != nil || inventory.InputBytes() != size || inventory.InputDigest() != archiveSHA256 {
		return Programmes{}, ErrArchive
	}
	read := func(name string) []byte { entry, _ := inventory.Entry(name); data, _ := entry.Bytes(); return data }
	allowed := make(map[string]bool)
	for name := range keep {
		allowed[name] = true
	}
	key, rawIndex, build, patch := read("signing-public-key.bin"), read("candidate-index.json"), read("build.json"), read("sdk-policy.patch")
	index, err := programmeIndex(rawIndex, key)
	if err != nil || sum(patch) != filecache.EnginePatchSHA256 || string(read("BUILD_COMPLETE")) != "unapproved candidate bundle\n" || !bytes.Equal(read("oci/oci-layout"), []byte(`{"imageLayoutVersion":"1.0.0"}`)) {
		return Programmes{}, ErrArchive
	}
	record, err := programmeBuildRecord(build, inventory.files)
	if err != nil {
		return Programmes{}, ErrArchive
	}
	expectedOCI := imageIndex{SchemaVersion: 2, MediaType: indexMedia}
	for _, entry := range index.Programmes {
		id := filecache.ArtifactID{Kind: entry.Kind, Architecture: entry.Architecture}
		name := string(id.Kind) + "-" + id.Architecture
		raw, signature := read("programmes/"+name+".json"), read("programmes/"+name+".sig")
		manifest, err := programmeManifest(id, raw, signature, key, entry.ManifestSHA256, sum(build))
		if err != nil {
			return Programmes{}, ErrArchive
		}
		objectName, ociName := "oci/blobs/sha256/"+manifest.ObjectSHA256, "oci/blobs/sha256/"+manifest.OCIManifestSHA256
		objectEntry, ok := inventory.Entry(objectName)
		if !ok {
			return Programmes{}, ErrArchive
		}
		object, err := entryData(source, objectEntry, filecache.MaxObjectBytes)
		if err != nil || sum(object) != manifest.ObjectSHA256 || filecache.ValidateObject(id.Kind, object) != nil {
			return Programmes{}, ErrArchive
		}
		ociEntry, ok := inventory.Entry(ociName)
		if !ok {
			return Programmes{}, ErrArchive
		}
		oci, err := entryData(source, ociEntry, 16384)
		if err != nil {
			return Programmes{}, ErrArchive
		}
		expected, err := filecache.OCIManifest(id, object)
		if err != nil || !bytes.Equal(expected, oci) || sum(oci) != manifest.OCIManifestSHA256 || record.objects[id] != manifest.ObjectSHA256 {
			return Programmes{}, ErrArchive
		}
		allowed[objectName], allowed[ociName] = true, true
		expectedOCI.Manifests = append(expectedOCI.Manifests, descriptor{MediaType: manifestMedia, Digest: "sha256:" + manifest.OCIManifestSHA256, Size: int64(len(oci)), Platform: &platform{Architecture: id.Architecture, OS: "linux"}, Annotations: map[string]string{"org.opencontainers.image.ref.name": name}})
	}
	emptyName := "oci/blobs/sha256/" + sum([]byte("{}"))
	emptyEntry, found := inventory.Entry(emptyName)
	if !found {
		return Programmes{}, ErrArchive
	}
	empty, err := entryData(source, emptyEntry, 2)
	if err != nil || string(empty) != "{}" {
		return Programmes{}, ErrArchive
	}
	allowed[emptyName] = true
	expected, err := json.Marshal(expectedOCI)
	if err != nil || !bytes.Equal(expected, read("oci/index.json")) {
		return Programmes{}, ErrArchive
	}
	for _, name := range inventory.Names() {
		entry, _ := inventory.Entry(name)
		if entry.Type == Regular && !allowed[name] {
			return Programmes{}, ErrArchive
		}
		if entry.Type == Directory && name != "source" && name != "programmes" && name != "oci" && name != "oci/blobs" && name != "oci/blobs/sha256" {
			return Programmes{}, ErrArchive
		}
	}
	return Programmes{files: inventory.files, digest: "sha256:" + sum(rawIndex), headers: record.headers}, nil
}
func programmeIndex(data, key []byte) (filecache.CandidateIndex, error) {
	var index filecache.CandidateIndex
	if len(key) != ed25519.PublicKeySize || decodeOCI(data, &index) != nil {
		return index, ErrArchive
	}
	canonical, err := json.Marshal(index)
	if err != nil || !bytes.Equal(canonical, data) || index.Version != 2 || index.Status != "unapproved" || index.PublicKeySHA256 != sum(key) || len(index.Programmes) != 6 {
		return index, ErrArchive
	}
	seen := make(map[filecache.ArtifactID]bool)
	for _, entry := range index.Programmes {
		id := filecache.ArtifactID{Kind: entry.Kind, Architecture: entry.Architecture}
		if !validProgrammeID(id) || !shaPattern.MatchString(entry.ManifestSHA256) || seen[id] {
			return index, ErrArchive
		}
		seen[id] = true
	}
	return index, nil
}
func validProgrammeID(id filecache.ArtifactID) bool {
	return (id.Architecture == "amd64" || id.Architecture == "arm64") && (id.Kind == trace.Files || id.Kind == trace.Cache || id.Kind == trace.OOM)
}
func programmeManifest(id filecache.ArtifactID, data, signature, key []byte, expected, buildSHA string) (filecache.Manifest, error) {
	var manifest filecache.Manifest
	if !validProgrammeID(id) || len(data) == 0 || len(data) > 2048 || len(key) != ed25519.PublicKeySize || len(signature) != ed25519.SignatureSize || sum(data) != expected || !ed25519.Verify(key, data, signature) || decodeOCI(data, &manifest) != nil {
		return manifest, ErrArchive
	}
	canonical, err := json.Marshal(manifest)
	if err != nil || !bytes.Equal(canonical, data) || manifest.Version != 1 || manifest.Kind != id.Kind || manifest.Architecture != id.Architecture || manifest.SourceSHA256 != buildSHA || !shaPattern.MatchString(buildSHA) || !shaPattern.MatchString(manifest.ObjectSHA256) || !shaPattern.MatchString(manifest.OCIManifestSHA256) || manifest.EngineCommit != filecache.EngineSourceCommit || manifest.EnginePatchSHA256 != filecache.EnginePatchSHA256 || manifest.BuilderDigest != filecache.BuilderDigest {
		return manifest, ErrArchive
	}
	return manifest, nil
}

func programmeImagePath(name string) string {
	return "opt/memlens-trace/programmes/" + name
}

func (p Programmes) Files() FileSet { return p.files }
