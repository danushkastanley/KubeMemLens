package workerinstall

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
)

func configuration(t *testing.T) Configuration {
	t.Helper()
	key, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	config := Configuration{Version: 1, Engine: EngineRelease{filecache.EngineSourceCommit, filecache.EnginePatchSHA256, map[string]string{"arm64": strings.Repeat("a", 64), "amd64": strings.Repeat("b", 64)}}, ProgrammeIndexSHA256: strings.Repeat("c", 64), ProgrammePublicKey: key,
		Programmes: []filecache.CandidateEntry{{Kind: "files", Architecture: "arm64", ManifestSHA256: strings.Repeat("d", 64)}}}
	signEngine(t, &config)
	return config
}

func signEngine(t *testing.T, config *Configuration) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(config.Engine)
	if err != nil {
		t.Fatal(err)
	}
	config.EnginePublicKey, config.EngineSignature = public, ed25519.Sign(private, data)
}

func encoded(t *testing.T, config Configuration) []byte {
	t.Helper()
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestInstallationIdentitiesAreFrozenAndArchitectureSpecific(t *testing.T) {
	config := configuration(t)
	data := encoded(t, config)
	policy, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	digest := policy.EngineDigest()
	config.Engine.Workers["arm64"] = strings.Repeat("e", 64)
	if _, err := Parse(encoded(t, config)); err == nil {
		t.Fatal("modified engine release accepted with old signature")
	}
	signEngine(t, &config)
	changed, err := Parse(encoded(t, config))
	if err != nil || changed.EngineDigest() == digest {
		t.Fatal("worker change did not change engine identity")
	}
	clear(data)
	if policy.EngineDigest() != digest || policy.ProgrammeDigest() != "sha256:"+strings.Repeat("c", 64) {
		t.Fatal("external bytes mutated policy")
	}
	if got, err := policy.WorkerSHA256("arm64"); err != nil || got != strings.Repeat("a", 64) {
		t.Fatal("arm64 selection changed")
	}
	if got, err := policy.WorkerSHA256("amd64"); err != nil || got != strings.Repeat("b", 64) {
		t.Fatal("amd64 selection changed")
	}
	if _, err := policy.WorkerSHA256("riscv64"); err == nil {
		t.Fatal("unaccepted architecture selected")
	}
	if _, err := policy.ManifestSHA256(filecache.ArtifactID{Kind: "cache", Architecture: "arm64"}); err == nil {
		t.Fatal("unaccepted kind selected")
	}
}

func TestInvalidInstallationChoicesAreRejected(t *testing.T) {
	for _, mutation := range []string{"empty-workers", "wrong-arch", "worker-digest", "patch", "source", "key", "duplicate", "missing-worker", "empty-accepted", "bundle-digest"} {
		t.Run(mutation, func(t *testing.T) {
			config := configuration(t)
			switch mutation {
			case "empty-workers":
				config.Engine.Workers = nil
			case "wrong-arch":
				config.Engine.Workers = map[string]string{"other": strings.Repeat("a", 64)}
			case "worker-digest":
				config.Engine.Workers["arm64"] = "bad"
			case "patch":
				config.Engine.PatchSHA256 = strings.Repeat("f", 64)
			case "source":
				config.Engine.SourceCommit = strings.Repeat("f", 40)
			case "key":
				config.ProgrammePublicKey = config.ProgrammePublicKey[:31]
			case "duplicate":
				config.Programmes = append(config.Programmes, config.Programmes[0])
			case "missing-worker":
				delete(config.Engine.Workers, "arm64")
			case "empty-accepted":
				config.Programmes = nil
			case "bundle-digest":
				config.ProgrammeIndexSHA256 = "bad"
			}
			if _, err := Parse(encoded(t, config)); err == nil {
				t.Fatal("invalid policy accepted")
			}
		})
	}
}

func TestPolicyCannotBeSuppliedByAmbiguousJSONOrCandidateIndex(t *testing.T) {
	valid := encoded(t, configuration(t))
	for _, data := range [][]byte{
		bytes.Replace(valid, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1),
		bytes.Replace(valid, []byte(`"engine"`), []byte(`"Engine"`), 1),
		append(append([]byte(nil), valid...), '\n'),
		[]byte(`{"version":1,"status":"unapproved","programmes":[]}`),
		make([]byte, MaxPolicyBytes+1),
	} {
		if _, err := Parse(data); err == nil {
			t.Fatal("ambiguous or non-policy input accepted")
		}
	}
}
