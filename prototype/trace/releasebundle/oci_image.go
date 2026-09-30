package releasebundle

import (
	"io"
	"slices"
	"strings"
	"time"
)

func (b *blobStore) platform(d descriptor, identity ImageIdentity) (FileSet, error) {
	var manifest imageManifest
	if b.readJSON(d, &manifest) != nil || manifest.SchemaVersion != 2 || manifest.MediaType != manifestMedia || len(manifest.Layers) == 0 || len(manifest.Layers) > 32 || !validAnnotations(manifest.Annotations) || manifest.Config.MediaType != configMedia || manifest.Config.Platform != nil {
		return FileSet{}, ErrArchive
	}
	var config imageConfig
	if b.readJSON(manifest.Config, &config) != nil || !validConfig(config, *d.Platform, identity, len(manifest.Layers)) {
		return FileSet{}, ErrArchive
	}
	files := FileSet{entries: make(map[string]Entry)}
	parents := make(map[string]bool)
	var expanded int64
	for index, layer := range manifest.Layers {
		if layer.Platform != nil || (layer.MediaType != layerMedia && layer.MediaType != gzipMedia) {
			return FileSet{}, ErrArchive
		}
		entry, err := b.entry(layer)
		if err != nil || entry.Size > 512<<20 {
			return FileSet{}, ErrArchive
		}
		compression := Tar
		if layer.MediaType == gzipMedia {
			compression = TarGzip
		}
		inventory, err := ReadArchive(io.NewSectionReader(b.source, entry.offset, entry.Size), compression, Limits{512 << 20, 1 << 30, 128 << 20, 20000}, nil)
		if err != nil || inventory.InputDigest() != entry.SHA256 || inventory.InputBytes() != entry.Size || "sha256:"+inventory.ExpandedDigest() != config.RootFS.DiffIDs[index] {
			return FileSet{}, ErrArchive
		}
		expanded += inventory.ExpandedBytes()
		if expanded > 1<<30 {
			return FileSet{}, ErrArchive
		}
		for _, name := range inventory.Names() {
			item, _ := inventory.Entry(name)
			if !traceImagePath(name) || !validParents(name, item.Type == Directory, files.entries, parents) {
				return FileSet{}, ErrArchive
			}
			previous, exists := files.entries[name]
			if exists && (previous.Type != Directory || item.Type != Directory || previous.Mode != item.Mode) {
				return FileSet{}, ErrArchive
			}
			if !exists {
				files.entries[name] = item
			}
			if len(files.entries) > 20000 {
				return FileSet{}, ErrArchive
			}
		}
	}
	for name, maximum := range map[string]int64{
		"memlens-trace": 128 << 20,
		"opt/memlens-trace/memlens-filecache-worker":           128 << 20,
		"opt/memlens-trace/programmes/candidate-index.json":    4096,
		"opt/memlens-trace/verification/engine-release.json":   4096,
		"opt/memlens-trace/verification/engine-release.sig":    64,
		"opt/memlens-trace/verification/engine-public-key.bin": 32,
	} {
		entry, found := files.Entry(name)
		if !found || entry.Type != Regular || entry.Size <= 0 || entry.Size > maximum {
			return FileSet{}, ErrArchive
		}
		if (name == "memlens-trace" || strings.HasSuffix(name, "/memlens-filecache-worker")) && entry.Mode&0111 == 0 {
			return FileSet{}, ErrArchive
		}
	}
	return files, nil
}
func traceImagePath(name string) bool {
	for _, part := range strings.Split(name, "/") {
		if strings.HasPrefix(part, ".wh.") {
			return false
		}
	}
	if name == "memlens-trace" || name == "opt" || name == "opt/memlens-trace" || name == "opt/memlens-trace/memlens-filecache-worker" {
		return true
	}
	for _, root := range []string{"licences", "sources", "opt/memlens-trace/reference", "opt/memlens-trace/programmes", "opt/memlens-trace/verification"} {
		if name == root || strings.HasPrefix(name, root+"/") {
			return true
		}
	}
	return false
}
func validConfig(config imageConfig, expected platform, identity ImageIdentity, layers int) bool {
	if !config.platform.valid() || config.Architecture != expected.Architecture || config.Author != "" || config.Created == nil || config.Created.IsZero() || config.RootFS.Type != "layers" || len(config.RootFS.DiffIDs) != layers {
		return false
	}
	// ArgsEscaped is Windows compatibility metadata. Both descriptor and config
	// have already been restricted to Linux; argv is checked explicitly below.
	runtime := config.Config
	if runtime.User != "65532:65532" || !slices.Equal(runtime.Entrypoint, []string{"/memlens-trace"}) || !slices.Equal(runtime.Cmd, []string{"doctor", "--json"}) || !validImageEnvironment(runtime.Env) || len(runtime.Volumes) != 0 || len(runtime.ExposedPorts) != 0 || (runtime.WorkingDir != "" && runtime.WorkingDir != "/") || (runtime.StopSignal != "" && runtime.StopSignal != "SIGTERM" && runtime.StopSignal != "15") || !validAnnotations(runtime.Labels) {
		return false
	}
	for name, expected := range map[string]string{
		"org.opencontainers.image.version":  identity.Version,
		"org.opencontainers.image.revision": identity.SourceCommit,
		"org.opencontainers.image.source":   "https://github.com/danushkastanley/KubeMemLens",
		"io.kubememlens.trace.contract":     "1",
	} {
		if runtime.Labels[name] != expected {
			return false
		}
	}
	created, err := time.Parse(time.RFC3339Nano, runtime.Labels["org.opencontainers.image.created"])
	if err != nil || !created.Equal(*config.Created) || len(config.History) == 0 || len(config.History) > 128 {
		return false
	}
	filesystemLayers := 0
	for _, item := range config.History {
		if item.Author != "" || !safeMetadata(item.CreatedBy) || !safeMetadata(item.Comment) {
			return false
		}
		if !item.EmptyLayer {
			filesystemLayers++
		}
	}
	if filesystemLayers != layers {
		return false
	}
	for _, digest := range config.RootFS.DiffIDs {
		if !validDigest(digest) {
			return false
		}
	}
	return true
}

// Dockerfile builders add this fixed PATH even to scratch images. Execution is
// still pinned to an absolute entrypoint. Other environment settings are refused.
func validImageEnvironment(values []string) bool {
	return len(values) == 0 || slices.Equal(values, []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"})
}
