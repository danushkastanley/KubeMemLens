// Package workerinstall owns immutable installation acceptance. Neither an
// admission request nor a candidate bundle can create or amend this policy.
package workerinstall

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
)

var ErrInstallation = errors.New("trace worker installation is not accepted")

const MaxPolicyBytes = 8192

// EngineRelease binds architecture-specific executable hashes under one common
// stream identity. It includes the separately reviewed SDK source and patch.
// Actual executable verification is required in addition to this declaration.
type EngineRelease struct {
	SourceCommit string            `json:"sourceCommit"`
	PatchSHA256  string            `json:"patchSHA256"`
	Workers      map[string]string `json:"workers"`
}

type Configuration struct {
	Version              int                        `json:"version"`
	Engine               EngineRelease              `json:"engine"`
	EnginePublicKey      []byte                     `json:"enginePublicKey"`
	EngineSignature      []byte                     `json:"engineSignature"`
	ProgrammeIndexSHA256 string                     `json:"programmeIndexSHA256"`
	ProgrammePublicKey   []byte                     `json:"programmePublicKey"`
	Programmes           []filecache.CandidateEntry `json:"programmes"`
}

type Policy struct {
	engineDigest string
	indexSHA256  string
	workers      map[string]string
	verifier     *filecache.Verifier
	encoded      []byte
}

func (*Policy) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "[worker installation policy]") }
func (*Policy) MarshalJSON() ([]byte, error) { return nil, ErrInstallation }

// Parse requires a canonical, bounded installation file. This validates the
// declared choices; only explicit maintainer acceptance permits installing it.
func Parse(data []byte) (*Policy, error) {
	if len(data) == 0 || len(data) > MaxPolicyBytes {
		return nil, ErrInstallation
	}
	var config Configuration
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&config) != nil {
		return nil, ErrInstallation
	}
	canonical, err := json.Marshal(config)
	if err != nil || !bytes.Equal(canonical, data) || config.Version != 1 ||
		config.Engine.SourceCommit != filecache.EngineSourceCommit || config.Engine.PatchSHA256 != filecache.EnginePatchSHA256 ||
		len(config.Engine.Workers) == 0 || len(config.Engine.Workers) > 2 || !validSHA(config.ProgrammeIndexSHA256) ||
		len(config.EnginePublicKey) != ed25519.PublicKeySize || len(config.EngineSignature) != ed25519.SignatureSize ||
		len(config.ProgrammePublicKey) != ed25519.PublicKeySize || len(config.Programmes) == 0 || len(config.Programmes) > 4 {
		return nil, ErrInstallation
	}
	for arch, digest := range config.Engine.Workers {
		if (arch != "arm64" && arch != "amd64") || !validSHA(digest) {
			return nil, ErrInstallation
		}
	}
	accepted := make(map[filecache.ArtifactID]string, len(config.Programmes))
	for _, entry := range config.Programmes {
		id := filecache.ArtifactID{Kind: entry.Kind, Architecture: entry.Architecture}
		if accepted[id] != "" || config.Engine.Workers[id.Architecture] == "" {
			return nil, ErrInstallation
		}
		accepted[id] = entry.ManifestSHA256
	}
	verifier, err := filecache.NewVerifier(config.ProgrammePublicKey, accepted)
	if err != nil {
		return nil, ErrInstallation
	}
	release, err := json.Marshal(config.Engine)
	if err != nil || !ed25519.Verify(config.EnginePublicKey, release, config.EngineSignature) {
		return nil, ErrInstallation
	}
	return &Policy{"sha256:" + sum(release), config.ProgrammeIndexSHA256, config.Engine.Workers, verifier, append([]byte(nil), data...)}, nil
}

func (p *Policy) EngineDigest() string    { return p.engineDigest }
func (p *Policy) ProgrammeDigest() string { return "sha256:" + p.indexSHA256 }
func (p *Policy) WorkerSHA256(architecture string) (string, error) {
	value := p.workers[architecture]
	if value == "" {
		return "", ErrInstallation
	}
	return value, nil
}
func (p *Policy) ManifestSHA256(id filecache.ArtifactID) (string, error) {
	value, err := p.verifier.ManifestDigest(id)
	if err != nil {
		return "", ErrInstallation
	}
	return value, nil
}
func validSHA(value string) bool {
	return len(value) == 64 && strings.Trim(value, "0123456789abcdef") == ""
}
func sum(data []byte) string { value := sha256.Sum256(data); return hex.EncodeToString(value[:]) }
