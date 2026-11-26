package releasebundle

import (
	"crypto/sha256"
	"encoding/hex"
	json "encoding/json/v2"
	"fmt"
	"io"
	"strings"
)

type Image struct {
	index     string
	platforms map[string]FileSet
}

func (Image) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "[release image inventory]") }
func (i Image) IndexDigest() string      { return i.index }
func (i Image) Platform(architecture string) (FileSet, bool) {
	value, ok := i.platforms[architecture]
	return value, ok
}

type blobStore struct {
	source    io.ReaderAt
	inventory Inventory
	used      map[string]bool
}

// InspectImage validates the exact archive/index digests and both platform file
// inventories. It neither verifies publisher signatures nor executes the image.
func InspectImage(source io.ReaderAt, size int64, archiveSHA256 string, identity ImageIdentity) (Image, error) {
	if source == nil || size <= 0 || size > 1<<30 || !shaPattern.MatchString(archiveSHA256) || !identity.valid() {
		return Image{}, ErrArchive
	}
	inventory, err := ReadArchive(io.NewSectionReader(source, 0, size), Tar, Limits{1 << 30, 1 << 30, 512 << 20, 1024}, map[string]int64{"index.json": 1 << 20, "oci-layout": 1024})
	if err != nil || inventory.InputDigest() != archiveSHA256 || inventory.InputBytes() != size {
		return Image{}, ErrArchive
	}
	store := blobStore{source: source, inventory: inventory, used: make(map[string]bool)}
	for _, name := range inventory.Names() {
		entry, _ := inventory.Entry(name)
		if entry.Type == Directory {
			if name != "blobs" && name != "blobs/sha256" {
				return Image{}, ErrArchive
			}
			continue
		}
		if name == "index.json" || name == "oci-layout" {
			continue
		}
		if !strings.HasPrefix(name, "blobs/sha256/") || !shaPattern.MatchString(strings.TrimPrefix(name, "blobs/sha256/")) || entry.SHA256 != strings.TrimPrefix(name, "blobs/sha256/") {
			return Image{}, ErrArchive
		}
	}
	layoutEntry, _ := inventory.Entry("oci-layout")
	layout, _ := layoutEntry.Bytes()
	var declared struct {
		Version string `json:"imageLayoutVersion"`
	}
	if decodeOCI(layout, &declared) != nil || declared.Version != "1.0.0" {
		return Image{}, ErrArchive
	}
	rootEntry, _ := inventory.Entry("index.json")
	rootBytes, _ := rootEntry.Bytes()
	var root imageIndex
	if decodeOCI(rootBytes, &root) != nil || !validIndex(root) || len(root.Manifests) != 1 {
		return Image{}, ErrArchive
	}
	descriptor := root.Manifests[0]
	if descriptor.Digest != identity.IndexDigest || descriptor.MediaType != indexMedia || descriptor.Platform != nil {
		return Image{}, ErrArchive
	}
	var index imageIndex
	if store.readJSON(descriptor, &index) != nil || !validIndex(index) || len(index.Manifests) != 2 {
		return Image{}, ErrArchive
	}
	result := Image{index: identity.IndexDigest, platforms: make(map[string]FileSet)}
	for _, item := range index.Manifests {
		if item.MediaType != manifestMedia || item.Platform == nil || !item.Platform.valid() {
			return Image{}, ErrArchive
		}
		architecture := item.Platform.Architecture
		if _, exists := result.platforms[architecture]; exists {
			return Image{}, ErrArchive
		}
		files, err := store.platform(item, identity)
		if err != nil {
			return Image{}, ErrArchive
		}
		result.platforms[architecture] = files
	}
	if len(result.platforms) != 2 {
		return Image{}, ErrArchive
	}
	for _, name := range inventory.Names() {
		entry, _ := inventory.Entry(name)
		if entry.Type == Regular && strings.HasPrefix(name, "blobs/") && !store.used[name] {
			return Image{}, ErrArchive
		}
	}
	return result, nil
}

func validIndex(index imageIndex) bool {
	return index.SchemaVersion == 2 && index.MediaType == indexMedia && validAnnotations(index.Annotations)
}
func decodeOCI(data []byte, value any) error {
	if len(data) == 0 || len(data) > 1<<20 || json.Unmarshal(data, value, json.RejectUnknownMembers(true)) != nil {
		return ErrArchive
	}
	return nil
}
func (b *blobStore) entry(d descriptor) (Entry, error) {
	if !validDigest(d.Digest) || d.Size <= 0 || !validAnnotations(d.Annotations) {
		return Entry{}, ErrArchive
	}
	name := "blobs/sha256/" + strings.TrimPrefix(d.Digest, "sha256:")
	entry, found := b.inventory.Entry(name)
	if !found || entry.Type != Regular || entry.Size != d.Size || "sha256:"+entry.SHA256 != d.Digest {
		return Entry{}, ErrArchive
	}
	b.used[name] = true
	return entry, nil
}
func (b *blobStore) readJSON(d descriptor, value any) error {
	entry, err := b.entry(d)
	if err != nil || entry.Size > 1<<20 {
		return ErrArchive
	}
	data, err := io.ReadAll(io.NewSectionReader(b.source, entry.offset, entry.Size))
	digest := sha256.Sum256(data)
	if err != nil || int64(len(data)) != entry.Size || hex.EncodeToString(digest[:]) != entry.SHA256 {
		return ErrArchive
	}
	return decodeOCI(data, value)
}
