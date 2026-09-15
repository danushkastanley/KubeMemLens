package filecache

import (
	"bytes"
	"encoding/json"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

type CandidateEntry struct {
	Kind           trace.Kind `json:"kind"`
	Architecture   string     `json:"architecture"`
	ManifestSHA256 string     `json:"manifestSHA256"`
}

// CandidateIndex describes build output. Its unapproved status is deliberately
// retained after installation; execution authority belongs to separate policy.
type CandidateIndex struct {
	Version         int              `json:"version"`
	Status          string           `json:"status"`
	PublicKeySHA256 string           `json:"publicKeySHA256"`
	Programmes      []CandidateEntry `json:"programmes"`
}

// CheckIndex binds the stream's common bundle identity to every independently
// accepted architecture/kind manifest. It cannot add entries to the allowlist.
func (v *Verifier) CheckIndex(data []byte, expectedSHA256 string) error {
	if v == nil || len(data) == 0 || len(data) > 4096 || !validSHA(expectedSHA256) || sumSHA(data) != expectedSHA256 {
		return ErrArtifact
	}
	var index CandidateIndex
	if json.Unmarshal(data, &index) != nil {
		return ErrArtifact
	}
	canonical, err := json.Marshal(index)
	if err != nil || !bytes.Equal(canonical, data) || index.Version != 1 || index.Status != "unapproved" || index.PublicKeySHA256 != sumSHA(v.key) || len(index.Programmes) != 4 {
		return ErrArtifact
	}
	entries := make(map[ArtifactID]string, 4)
	for _, entry := range index.Programmes {
		id := ArtifactID{entry.Kind, entry.Architecture}
		if !validArtifactID(id) || !validSHA(entry.ManifestSHA256) || entries[id] != "" {
			return ErrArtifact
		}
		entries[id] = entry.ManifestSHA256
	}
	for id, digest := range v.accepted {
		if entries[id] != digest {
			return ErrArtifact
		}
	}
	return nil
}

// ManifestDigest reports only independently accepted entries, never bundle
// contents. The returned digest is copied and cannot alter verifier policy.
func (v *Verifier) ManifestDigest(id ArtifactID) (string, error) {
	if v == nil {
		return "", ErrArtifact
	}
	digest, ok := v.accepted[id]
	if !ok {
		return "", ErrArtifact
	}
	return digest, nil
}
