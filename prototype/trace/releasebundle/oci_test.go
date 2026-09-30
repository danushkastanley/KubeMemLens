package releasebundle

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestImageBindsBothArchitecturesAndExactLayerContents(t *testing.T) {
	raw, identity := imageFixture(t, imageChanges{})
	result, err := InspectImage(bytes.NewReader(raw), int64(len(raw)), digest(raw), identity)
	if err != nil || result.IndexDigest() != identity.IndexDigest {
		t.Fatalf("valid image rejected: %v", err)
	}
	for _, arch := range []string{"amd64", "arm64"} {
		platform, ok := result.Platform(arch)
		entry, found := platform.Entry("memlens-trace")
		if !ok || !found || entry.SHA256 != digest([]byte("synthetic launcher "+arch)) {
			t.Fatal("wrong platform content")
		}
	}
	if _, found := result.Platform("ppc64le"); found {
		t.Fatal("unexpected platform")
	}
}
func TestImageAcceptsDockerfileLinuxMetadataWithoutChangingArgv(t *testing.T) {
	raw, identity := imageFixture(t, imageChanges{config: func(_ string, c *imageConfig) {
		c.Config.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}
		c.Config.ArgsEscaped = true
	}})
	if _, err := InspectImage(bytes.NewReader(raw), int64(len(raw)), digest(raw), identity); err != nil {
		t.Fatal(err)
	}
}

func TestImageRejectsRetargetingAndMismatchedMetadata(t *testing.T) {
	cases := map[string]imageChanges{
		"missing platform":   {index: func(i *imageIndex) { i.Manifests = i.Manifests[:1] }},
		"duplicate platform": {index: func(i *imageIndex) { i.Manifests[1].Platform = i.Manifests[0].Platform }},
		"foreign platform":   {index: func(i *imageIndex) { i.Manifests[0].Platform.OS = "windows" }},
		"wrong config arch":  {config: func(_ string, c *imageConfig) { c.Architecture = "arm" }},
		"root user":          {config: func(_ string, c *imageConfig) { c.Config.User = "0" }},
		"entrypoint":         {config: func(_ string, c *imageConfig) { c.Config.Entrypoint = []string{"/bin/sh"} }},
		"command":            {config: func(_ string, c *imageConfig) { c.Config.Cmd = []string{"binding-node"} }},
		"environment":        {config: func(_ string, c *imageConfig) { c.Config.Env = []string{"LD_PRELOAD=/evil"} }},
		"volume":             {config: func(_ string, c *imageConfig) { c.Config.Volumes = map[string]struct{}{"/host": {}} }},
		"version":            {config: func(_ string, c *imageConfig) { c.Config.Labels["org.opencontainers.image.version"] = "0.1.0-dev.2" }},
		"commit": {config: func(_ string, c *imageConfig) {
			c.Config.Labels["org.opencontainers.image.revision"] = strings.Repeat("c", 40)
		}},
		"contract":              {config: func(_ string, c *imageConfig) { c.Config.Labels["io.kubememlens.trace.contract"] = "2" }},
		"diff id":               {config: func(_ string, c *imageConfig) { c.RootFS.DiffIDs[0] = "sha256:" + strings.Repeat("c", 64) }},
		"history count":         {config: func(_ string, c *imageConfig) { c.History[0].EmptyLayer = true }},
		"private history":       {config: func(_ string, c *imageConfig) { c.History[0].CreatedBy = "COPY /Users/private/key" }},
		"unexpected mountpoint": {files: func(_ string, f []member) []member { return append(f, file("etc/secret", "x")) }},
		"whiteout":              {files: func(_ string, f []member) []member { return append(f, file("sources/.wh.deleted", "")) }},
		"missing worker":        {files: func(_ string, f []member) []member { return append(f[:1], f[2:]...) }},
		"unexecutable worker":   {files: func(_ string, f []member) []member { f[1].header.Mode = 0644; return f }},
		"wrong descriptor size": {manifest: func(_ string, m *imageManifest) { m.Config.Size++ }},
		"foreign layer": {manifest: func(_ string, m *imageManifest) {
			m.Layers[0].MediaType = "application/vnd.docker.image.rootfs.foreign.diff.tar.gzip"
		}},
		"embedded descriptor": {root: func(r map[string]any) { r["manifests"].([]any)[0].(map[string]any)["data"] = "e30=" }},
		"remote descriptor": {root: func(r map[string]any) {
			r["manifests"].([]any)[0].(map[string]any)["urls"] = []string{"https://example.invalid/blob"}
		}},
		"orphan blob": {blobs: func(b map[string][]byte) { data := []byte("orphan"); b[digest(data)] = data }},
		"tampered blob": {blobs: func(b map[string][]byte) {
			for name := range b {
				b[name] = []byte("altered")
				break
			}
		}},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			raw, identity := imageFixture(t, change)
			if _, err := InspectImage(bytes.NewReader(raw), int64(len(raw)), digest(raw), identity); !errors.Is(err, ErrArchive) {
				t.Fatalf("unsafe image accepted: %v", err)
			}
		})
	}
}
func TestImageRejectsWrongOuterAndIndexDigests(t *testing.T) {
	raw, identity := imageFixture(t, imageChanges{})
	if _, err := InspectImage(bytes.NewReader(raw), int64(len(raw)), strings.Repeat("0", 64), identity); !errors.Is(err, ErrArchive) {
		t.Fatal("wrong archive accepted")
	}
	if _, err := InspectImage(bytes.NewReader(raw), int64(len(raw)+512), digest(raw), identity); !errors.Is(err, ErrArchive) {
		t.Fatal("incorrect declared archive size accepted")
	}
	identity.IndexDigest = "sha256:" + strings.Repeat("0", 64)
	if _, err := InspectImage(bytes.NewReader(raw), int64(len(raw)), digest(raw), identity); !errors.Is(err, ErrArchive) {
		t.Fatal("wrong index accepted")
	}
}

type changingReader struct {
	data                []byte
	initialReadComplete bool
}

func (r *changingReader) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(r.data)) {
		r.initialReadComplete = true
		return 0, io.EOF
	}
	n := copy(p, r.data[off:])
	if r.initialReadComplete && n > 0 {
		p[0] ^= 1
	}
	if off+int64(n) == int64(len(r.data)) {
		r.initialReadComplete = true
	}
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}
func TestImageRechecksMetadataReadAfterArchiveHash(t *testing.T) {
	raw, identity := imageFixture(t, imageChanges{})
	if _, err := InspectImage(&changingReader{data: raw}, int64(len(raw)), digest(raw), identity); !errors.Is(err, ErrArchive) {
		t.Fatal("changing archive metadata accepted")
	}
}
