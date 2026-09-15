package filecache

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func TestIndexBindsAcceptedSubsetWithoutGrantingOtherEntries(t *testing.T) {
	key, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	index := CandidateIndex{Version: 1, Status: "unapproved", PublicKeySHA256: sumSHA(key)}
	for _, arch := range []string{"amd64", "arm64"} {
		for _, kind := range []trace.Kind{trace.Files, trace.Cache} {
			index.Programmes = append(index.Programmes, CandidateEntry{kind, arch, strings.Repeat("a", 64)})
		}
	}
	id := ArtifactID{trace.Files, "arm64"}
	verifier, err := NewVerifier(key, map[ArtifactID]string{id: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	if verifier.CheckIndex(data, sumSHA(data)) != nil {
		t.Fatal("valid bundle index rejected")
	}
	if _, err := verifier.ManifestDigest(ArtifactID{trace.Cache, "arm64"}); err == nil {
		t.Fatal("bundle index granted unaccepted programme")
	}
	for _, mutation := range []string{"duplicate", "missing", "key", "manifest", "status", "alias"} {
		t.Run(mutation, func(t *testing.T) {
			copy := index
			copy.Programmes = append([]CandidateEntry(nil), index.Programmes...)
			switch mutation {
			case "duplicate":
				copy.Programmes[0] = copy.Programmes[1]
			case "missing":
				copy.Programmes = copy.Programmes[:3]
			case "key":
				copy.PublicKeySHA256 = strings.Repeat("f", 64)
			case "manifest":
				copy.Programmes[2].ManifestSHA256 = strings.Repeat("f", 64)
			case "status":
				copy.Status = "approved"
			}
			bad, err := json.Marshal(copy)
			if err != nil {
				t.Fatal(err)
			}
			if mutation == "alias" {
				bad = bytes.Replace(bad, []byte(`"version"`), []byte(`"Version"`), 1)
			}
			if verifier.CheckIndex(bad, sumSHA(bad)) == nil {
				t.Fatal("invalid bundle identity accepted")
			}
		})
	}
}
