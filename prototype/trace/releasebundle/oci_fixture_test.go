package releasebundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"
)

type imageChanges struct {
	files    func(string, []member) []member
	config   func(string, *imageConfig)
	manifest func(string, *imageManifest)
	index    func(*imageIndex)
	root     func(map[string]any)
	blobs    func(map[string][]byte)
}

func digest(data []byte) string { value := sha256.Sum256(data); return hex.EncodeToString(value[:]) }
func imageFixture(t testing.TB, change imageChanges) ([]byte, ImageIdentity) {
	t.Helper()
	blobs := make(map[string][]byte)
	put := func(media string, data []byte) descriptor {
		sha := digest(data)
		blobs[sha] = data
		return descriptor{MediaType: media, Digest: "sha256:" + sha, Size: int64(len(data))}
	}
	encode := func(value any) []byte {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	identity := ImageIdentity{Version: "0.1.0-dev.1", SourceCommit: strings.Repeat("a", 40)}
	index := imageIndex{SchemaVersion: 2, MediaType: indexMedia}
	for _, arch := range []string{"amd64", "arm64"} {
		files := []member{
			file("memlens-trace", "synthetic launcher "+arch),
			file("opt/memlens-trace/memlens-filecache-worker", "synthetic worker "+arch),
			file("opt/memlens-trace/programmes/candidate-index.json", "{}"),
			file("opt/memlens-trace/verification/engine-release.json", "{}"),
			file("opt/memlens-trace/verification/engine-release.sig", strings.Repeat("s", 64)),
			file("opt/memlens-trace/verification/engine-public-key.bin", strings.Repeat("k", 32)),
		}
		files[0].header.Mode, files[1].header.Mode = 0555, 0555
		if change.files != nil {
			files = change.files(arch, files)
		}
		raw := archive(t, files...)
		layer := put(layerMedia, raw)
		if arch == "arm64" {
			layer = put(gzipMedia, compressed(t, raw))
			delete(blobs, digest(raw))
		}
		created := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
		config := imageConfig{Created: &created, platform: platform{Architecture: arch, OS: "linux"},
			Config: runtimeConfig{User: "65532:65532", Entrypoint: []string{"/memlens-trace"}, Cmd: []string{"doctor", "--json"}, Labels: map[string]string{
				"org.opencontainers.image.version": identity.Version, "org.opencontainers.image.revision": identity.SourceCommit,
				"org.opencontainers.image.source":  "https://github.com/danushkastanley/KubeMemLens",
				"org.opencontainers.image.created": created.Format(time.RFC3339), "io.kubememlens.trace.contract": "1",
			}}, History: []history{{Created: &created, CreatedBy: "COPY public fixture"}}}
		config.RootFS.Type, config.RootFS.DiffIDs = "layers", []string{"sha256:" + digest(raw)}
		if change.config != nil {
			change.config(arch, &config)
		}
		manifest := imageManifest{SchemaVersion: 2, MediaType: manifestMedia, Config: put(configMedia, encode(config)), Layers: []descriptor{layer}}
		if change.manifest != nil {
			change.manifest(arch, &manifest)
		}
		d := put(manifestMedia, encode(manifest))
		d.Platform = &platform{Architecture: arch, OS: "linux"}
		index.Manifests = append(index.Manifests, d)
	}
	if change.index != nil {
		change.index(&index)
	}
	indexDescriptor := put(indexMedia, encode(index))
	identity.IndexDigest = indexDescriptor.Digest
	root := imageIndex{SchemaVersion: 2, MediaType: indexMedia, Manifests: []descriptor{indexDescriptor}}
	rootBytes := encode(root)
	if change.root != nil {
		var value map[string]any
		if err := json.Unmarshal(rootBytes, &value); err != nil {
			t.Fatal(err)
		}
		change.root(value)
		rootBytes = encode(value)
	}
	if change.blobs != nil {
		change.blobs(blobs)
	}
	members := []member{file("oci-layout", `{"imageLayoutVersion":"1.0.0"}`), file("index.json", string(rootBytes))}
	keys := make([]string, 0, len(blobs))
	for key := range blobs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		members = append(members, file("blobs/sha256/"+key, string(blobs[key])))
	}
	return archive(t, members...), identity
}
