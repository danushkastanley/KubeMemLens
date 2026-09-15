//go:build linux || darwin

package workerinstall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
)

func TestPolicyBindsActualCandidateIndexAndRequestedManifest(t *testing.T) {
	directory := os.Getenv("KML_REVIEW_BUNDLE")
	if directory == "" {
		t.Skip("requires retained signed review bundle")
	}
	data, err := os.ReadFile(filepath.Join(directory, "candidate-index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var index filecache.CandidateIndex
	if json.Unmarshal(data, &index) != nil {
		t.Fatal("candidate index invalid")
	}
	key, err := os.ReadFile(filepath.Join(directory, "signing-public-key.bin"))
	if err != nil {
		t.Fatal(err)
	}
	config := configuration(t)
	config.ProgrammeIndexSHA256, config.ProgrammePublicKey, config.Programmes = sum(data), key, index.Programmes
	// Test-local declaration only: no policy is installed and no worker runs.
	policy, err := Parse(encoded(t, config))
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, entry := range index.Programmes {
		id := filecache.ArtifactID{Kind: entry.Kind, Architecture: entry.Architecture}
		programme, err := policy.Programme(root, id)
		if err != nil || programme.Manifest().Kind != entry.Kind || programme.Manifest().Architecture != entry.Architecture {
			t.Fatal("accepted candidate did not match requested identity")
		}
	}
	config.Programmes[0].ManifestSHA256 = strings.Repeat("f", 64)
	wrong, err := Parse(encoded(t, config))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrong.Programme(root, filecache.ArtifactID{Kind: "files", Architecture: "arm64"}); err == nil {
		t.Fatal("index not checked against every accepted entry")
	}
	config.ProgrammeIndexSHA256 = strings.Repeat("e", 64)
	wrong, err = Parse(encoded(t, config))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrong.Programme(root, filecache.ArtifactID{Kind: "files", Architecture: "arm64"}); err == nil {
		t.Fatal("wrong bundle identity accepted")
	}
}
