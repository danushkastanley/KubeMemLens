package filecache

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
)

// Candidate is signed build output, not an accepted or executable Programme.
// SourceSHA256 identifies the complete retained build record. Candidate signing
// cannot add an entry to the independent installation acceptance allowlist.
type Candidate struct {
	Manifest  []byte
	Signature []byte
	Object    []byte
	OCI       []byte
}

func (Candidate) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[unapproved programme candidate]")
}
func (Candidate) MarshalJSON() ([]byte, error) { return nil, ErrArtifact }

func BuildCandidate(id ArtifactID, object []byte, sourceSHA256 string, key ed25519.PrivateKey) (Candidate, error) {
	if !validArtifactID(id) || !validSHA(sourceSHA256) || len(key) != ed25519.PrivateKeySize || ValidateObject(id.Kind, object) != nil {
		return Candidate{}, ErrArtifact
	}
	// Reject inconsistent seed/public-key material before producing a signature.
	derived := ed25519.NewKeyFromSeed(key.Seed())
	if !key.Equal(derived) {
		return Candidate{}, ErrArtifact
	}
	oci, err := OCIManifest(id, object)
	if err != nil {
		return Candidate{}, err
	}
	manifest, err := json.Marshal(Manifest{Version: 1, Kind: id.Kind, Architecture: id.Architecture,
		ObjectSHA256: sumSHA(object), SourceSHA256: sourceSHA256, OCIManifestSHA256: sumSHA(oci),
		EngineCommit: EngineSourceCommit, BuilderDigest: BuilderDigest, EnginePatchSHA256: EnginePatchSHA256})
	if err != nil {
		return Candidate{}, ErrArtifact
	}
	return Candidate{manifest, ed25519.Sign(key, manifest), append([]byte(nil), object...), oci}, nil
}
