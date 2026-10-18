package releasebundle

import (
	"regexp"
	"strings"
	"time"
	"unicode"
)

const (
	indexMedia    = "application/vnd.oci.image.index.v1+json"
	manifestMedia = "application/vnd.oci.image.manifest.v1+json"
	configMedia   = "application/vnd.oci.image.config.v1+json"
	layerMedia    = "application/vnd.oci.image.layer.v1.tar"
	gzipMedia     = layerMedia + "+gzip"
)

var shaPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var commitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-(dev|beta)\.[1-9][0-9]*$`)

type ImageIdentity struct {
	Version, SourceCommit, IndexDigest string
}

func (i ImageIdentity) valid() bool {
	return len(i.Version) <= 64 && versionPattern.MatchString(i.Version) && commitPattern.MatchString(i.SourceCommit) && validDigest(i.IndexDigest)
}
func validDigest(value string) bool {
	return strings.HasPrefix(value, "sha256:") && shaPattern.MatchString(strings.TrimPrefix(value, "sha256:"))
}

// These wire types describe the deliberately restricted trace image profile.
// External URLs, embedded blobs, foreign platforms and inline attestations are
// outside this reproducible profile, even where the general OCI format allows them.
type descriptor struct {
	MediaType   string            `json:"mediaType"`
	Digest      string            `json:"digest"`
	Size        int64             `json:"size"`
	Platform    *platform         `json:"platform,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}
type platform struct {
	Architecture string   `json:"architecture"`
	OS           string   `json:"os"`
	Variant      string   `json:"variant,omitempty"`
	OSVersion    string   `json:"os.version,omitempty"`
	OSFeatures   []string `json:"os.features,omitempty"`
}

func (p platform) valid() bool {
	return p.OS == "linux" && p.OSVersion == "" && len(p.OSFeatures) == 0 &&
		((p.Architecture == "amd64" && p.Variant == "") || (p.Architecture == "arm64" && (p.Variant == "" || p.Variant == "v8")))
}

type imageIndex struct {
	SchemaVersion int               `json:"schemaVersion"`
	MediaType     string            `json:"mediaType"`
	Manifests     []descriptor      `json:"manifests"`
	Annotations   map[string]string `json:"annotations,omitempty"`
}
type imageManifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	MediaType     string            `json:"mediaType"`
	Config        descriptor        `json:"config"`
	Layers        []descriptor      `json:"layers"`
	Annotations   map[string]string `json:"annotations,omitempty"`
}
type imageConfig struct {
	Created *time.Time `json:"created,omitempty"`
	Author  string     `json:"author,omitempty"`
	platform
	Config runtimeConfig `json:"config"`
	RootFS struct {
		Type    string   `json:"type"`
		DiffIDs []string `json:"diff_ids"`
	} `json:"rootfs"`
	History []history `json:"history,omitempty"`
}
type runtimeConfig struct {
	User         string              `json:"User"`
	Entrypoint   []string            `json:"Entrypoint"`
	Cmd          []string            `json:"Cmd"`
	Env          []string            `json:"Env,omitempty"`
	WorkingDir   string              `json:"WorkingDir,omitempty"`
	Labels       map[string]string   `json:"Labels,omitempty"`
	ExposedPorts map[string]struct{} `json:"ExposedPorts,omitempty"`
	Volumes      map[string]struct{} `json:"Volumes,omitempty"`
	StopSignal   string              `json:"StopSignal,omitempty"`
	ArgsEscaped  bool                `json:"ArgsEscaped,omitempty"`
}
type history struct {
	Created    *time.Time `json:"created,omitempty"`
	CreatedBy  string     `json:"created_by,omitempty"`
	Author     string     `json:"author,omitempty"`
	Comment    string     `json:"comment,omitempty"`
	EmptyLayer bool       `json:"empty_layer,omitempty"`
}

func safeMetadata(value string) bool {
	if len(value) > 4096 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return false
	}
	for _, private := range []string{"/Users/", "/home/runner/", "/private/", "/workspace/", "PRIVATE KEY-----", "Bearer "} {
		if strings.Contains(value, private) {
			return false
		}
	}
	return true
}
func validAnnotations(values map[string]string) bool {
	if len(values) > 32 {
		return false
	}
	for key, value := range values {
		if !safeMetadata(key) || !safeMetadata(value) {
			return false
		}
	}
	return true
}
