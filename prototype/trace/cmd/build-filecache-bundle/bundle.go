package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
)

type entry = filecache.CandidateEntry
type candidateIndex = filecache.CandidateIndex
type indexDescriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int    `json:"size"`
	Platform  struct {
		Architecture string `json:"architecture"`
		OS           string `json:"os"`
	} `json:"platform"`
	Annotations map[string]string `json:"annotations"`
}
type ociIndex struct {
	SchemaVersion int               `json:"schemaVersion"`
	MediaType     string            `json:"mediaType"`
	Manifests     []indexDescriptor `json:"manifests"`
}

func buildBundle(build, output, patch string, key ed25519.PrivateKey) error {
	input, err := readInputs(build, patch)
	if err != nil {
		return err
	}
	if newDirectory(output) != nil {
		return errBuild
	}
	for _, name := range []string{"source", "programmes", "oci", "oci/blobs", "oci/blobs/sha256"} {
		if newDirectory(filepath.Join(output, name)) != nil {
			return errBuild
		}
	}
	for name, data := range input.sources {
		if writeNew(filepath.Join(output, "source", name), data) != nil {
			return errBuild
		}
	}
	for name, data := range map[string][]byte{"build.json": input.record, "sdk-policy.patch": input.patch, "signing-public-key.bin": key.Public().(ed25519.PublicKey), "oci/oci-layout": []byte(`{"imageLayoutVersion":"1.0.0"}`)} {
		if writeNew(filepath.Join(output, name), data) != nil {
			return errBuild
		}
	}
	if writeBlob(output, []byte("{}")) != nil {
		return errBuild
	}
	index := candidateIndex{Version: 1, Status: "unapproved", PublicKeySHA256: sha(key.Public().(ed25519.PublicKey))}
	if containsKind(input.kinds, trace.OOM) {
		index.Version = 2
	}
	oci := ociIndex{SchemaVersion: 2, MediaType: "application/vnd.oci.image.index.v1+json"}
	for _, arch := range []string{"amd64", "arm64"} {
		for _, kind := range input.kinds {
			id := filecache.ArtifactID{Kind: kind, Architecture: arch}
			candidate, err := filecache.BuildCandidate(id, input.objects[id], sha(input.record), key)
			if err != nil {
				return errBuild
			}
			name := string(kind) + "-" + arch
			for suffix, data := range map[string][]byte{".json": candidate.Manifest, ".sig": candidate.Signature} {
				if writeNew(filepath.Join(output, "programmes", name+suffix), data) != nil {
					return errBuild
				}
			}
			if writeBlob(output, candidate.Object) != nil || writeBlob(output, candidate.OCI) != nil {
				return errBuild
			}
			index.Programmes = append(index.Programmes, entry{Kind: kind, Architecture: arch, ManifestSHA256: sha(candidate.Manifest)})
			d := indexDescriptor{MediaType: filecache.OCIManifestMediaType, Digest: "sha256:" + sha(candidate.OCI), Size: len(candidate.OCI), Annotations: map[string]string{"org.opencontainers.image.ref.name": name}}
			d.Platform.Architecture, d.Platform.OS = arch, "linux"
			oci.Manifests = append(oci.Manifests, d)
		}
	}
	for name, value := range map[string]any{"oci/index.json": oci, "candidate-index.json": index} {
		data, err := json.Marshal(value)
		if err != nil || writeNew(filepath.Join(output, name), data) != nil {
			return errBuild
		}
	}
	// Completion receipt is written last. It grants no execution acceptance.
	return writeNew(filepath.Join(output, "BUILD_COMPLETE"), []byte("unapproved candidate bundle\n"))
}

func writeBlob(root string, data []byte) error {
	path := filepath.Join(root, "oci", "blobs", "sha256", sha(data))
	if _, err := os.Lstat(path); err == nil {
		existing, err := readBounded(path, filecache.MaxObjectBytes)
		if err != nil || !bytes.Equal(existing, data) {
			return errBuild
		}
		return nil
	} else if !os.IsNotExist(err) {
		return errBuild
	}
	return writeNew(path, data)
}
