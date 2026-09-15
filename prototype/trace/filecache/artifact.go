package filecache

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

const EngineSourceCommit = "e5a2855f270ca6557f4bd7e4fabaddf6760d8f50"
const BuilderDigest = "sha256:d55d33bfd2583e721ac78972a8039fb4b207d157e06f0c623c5f8a55cee597b4"
const EnginePatchSHA256 = "34583005d8d808e68fd5f34dae4da9073516516e090fda3aafcbed7abef022af"

var ErrArtifact = errors.New("file/cache programme is not approved")

type ArtifactID struct {
	Kind         trace.Kind
	Architecture string
}

// Manifest is a canonical signed review record. An accepted manifest digest
// binds both the exact object and its build/source/OCI identities. A valid
// signature alone never grants permission to execute a different programme.
type Manifest struct {
	Version           int        `json:"version"`
	Kind              trace.Kind `json:"kind"`
	Architecture      string     `json:"architecture"`
	ObjectSHA256      string     `json:"objectSHA256"`
	SourceSHA256      string     `json:"sourceSHA256"`
	OCIManifestSHA256 string     `json:"ociManifestSHA256"`
	EngineCommit      string     `json:"engineCommit"`
	BuilderDigest     string     `json:"builderDigest"`
	EnginePatchSHA256 string     `json:"enginePatchSHA256"`
}

type Verifier struct {
	key      ed25519.PublicKey
	accepted map[ArtifactID]string
}

// NewVerifier is installation configuration, never an admission request field.
// Callers must obtain explicit acceptance of these manifest digests first.
func NewVerifier(key ed25519.PublicKey, accepted map[ArtifactID]string) (*Verifier, error) {
	if len(key) != ed25519.PublicKeySize || len(accepted) == 0 || len(accepted) > 4 {
		return nil, ErrArtifact
	}
	v := &Verifier{key: append(ed25519.PublicKey(nil), key...), accepted: make(map[ArtifactID]string, len(accepted))}
	for id, digest := range accepted {
		if !validArtifactID(id) || !validSHA(digest) {
			return nil, ErrArtifact
		}
		v.accepted[id] = digest
	}
	return v, nil
}

// Programme can only be constructed after signature, independent acceptance,
// object digest and ABI checks. It owns a copy of the verified object bytes.
type Programme struct {
	manifest Manifest
	object   []byte
}

func (*Programme) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[verified file/cache programme]")
}
func (*Programme) MarshalJSON() ([]byte, error) { return nil, ErrArtifact }
func (p *Programme) Manifest() Manifest         { return p.manifest }
func (p *Programme) Object() []byte             { return append([]byte(nil), p.object...) }

func (v *Verifier) Verify(id ArtifactID, manifest, signature, object, ociManifest []byte) (*Programme, error) {
	m, err := v.verifyManifest(id, manifest, signature)
	if err != nil {
		return nil, err
	}
	return verifyContent(id, m, object, ociManifest)
}

func (v *Verifier) verifyManifest(id ArtifactID, manifest, signature []byte) (Manifest, error) {
	if v == nil || !validArtifactID(id) || len(manifest) == 0 || len(manifest) > 2048 || len(signature) != ed25519.SignatureSize {
		return Manifest{}, ErrArtifact
	}
	expected, accepted := v.accepted[id]
	if !accepted || sumSHA(manifest) != expected || !ed25519.Verify(v.key, manifest, signature) {
		return Manifest{}, ErrArtifact
	}
	var m Manifest
	decoder := json.NewDecoder(bytes.NewReader(manifest))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&m) != nil {
		return Manifest{}, ErrArtifact
	}
	canonical, err := json.Marshal(m)
	if err != nil || !bytes.Equal(canonical, manifest) || m.Version != 1 || m.Kind != id.Kind || m.Architecture != id.Architecture ||
		m.EngineCommit != EngineSourceCommit || m.EnginePatchSHA256 != EnginePatchSHA256 || m.BuilderDigest != BuilderDigest || !validSHA(m.SourceSHA256) ||
		!validSHA(m.ObjectSHA256) || !validSHA(m.OCIManifestSHA256) {
		return Manifest{}, ErrArtifact
	}
	return m, nil
}

func verifyContent(id ArtifactID, m Manifest, object, ociManifest []byte) (*Programme, error) {
	if len(object) == 0 || len(object) > MaxObjectBytes || len(ociManifest) == 0 || len(ociManifest) > 16384 || m.ObjectSHA256 != sumSHA(object) || m.OCIManifestSHA256 != sumSHA(ociManifest) {
		return nil, ErrArtifact
	}
	expectedOCI, err := OCIManifest(id, object)
	if err != nil || !bytes.Equal(expectedOCI, ociManifest) || ValidateObject(id.Kind, object) != nil {
		return nil, ErrArtifact
	}
	return &Programme{m, append([]byte(nil), object...)}, nil
}

func validArtifactID(id ArtifactID) bool {
	return (id.Kind == trace.Files || id.Kind == trace.Cache) && (id.Architecture == "amd64" || id.Architecture == "arm64")
}
func validSHA(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
func sumSHA(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
