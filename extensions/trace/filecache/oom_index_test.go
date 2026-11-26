package filecache

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func TestOOMIndexRequiresNewVersionAndIndependentAcceptance(t *testing.T) {
	key, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	index := CandidateIndex{Version: 2, Status: "unapproved", PublicKeySHA256: sumSHA(key)}
	for _, arch := range []string{"amd64", "arm64"} {
		for _, kind := range []trace.Kind{trace.Files, trace.Cache, trace.OOM} {
			index.Programmes = append(index.Programmes, CandidateEntry{kind, arch, strings.Repeat("a", 64)})
		}
	}
	fileID, oomID := ArtifactID{trace.Files, "arm64"}, ArtifactID{trace.OOM, "arm64"}
	verifier, err := NewVerifier(key, map[ArtifactID]string{fileID: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(index)
	if verifier.CheckIndex(data, sumSHA(data)) != nil {
		t.Fatal("valid six-object index rejected")
	}
	if _, err := verifier.ManifestDigest(oomID); err == nil {
		t.Fatal("bundle presence granted OOM acceptance")
	}
	for _, mutation := range []string{"old-version", "missing-architecture", "duplicate", "unknown-kind"} {
		t.Run(mutation, func(t *testing.T) {
			changed := index
			changed.Programmes = append([]CandidateEntry(nil), index.Programmes...)
			switch mutation {
			case "old-version":
				changed.Version = 1
			case "missing-architecture":
				changed.Programmes = changed.Programmes[:5]
			case "duplicate":
				changed.Programmes[0] = changed.Programmes[1]
			case "unknown-kind":
				changed.Programmes[0].Kind = "arbitrary"
			}
			data, _ := json.Marshal(changed)
			if verifier.CheckIndex(data, sumSHA(data)) == nil {
				t.Fatal("invalid OOM programme index accepted")
			}
		})
	}
	// A four-entry legacy index cannot hide OOM entries behind its old version.
	index.Version = 1
	index.Programmes = index.Programmes[:4]
	data, _ = json.Marshal(index)
	if verifier.CheckIndex(data, sumSHA(data)) == nil {
		t.Fatal("legacy index accepted OOM entries")
	}
}
