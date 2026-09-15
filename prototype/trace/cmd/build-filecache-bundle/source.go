package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
)

type objectRecord struct {
	Kind         trace.Kind `json:"kind"`
	Architecture string     `json:"architecture"`
	SHA256       string     `json:"sha256"`
	Path         string     `json:"path"`
	Reproduced   bool       `json:"reproduced"`
}
type buildRecord struct {
	Builder        string            `json:"builder"`
	UpstreamCommit string            `json:"upstreamCommit"`
	SourceSHA256   map[string]string `json:"sourceSHA256"`
	HeaderSHA256   map[string]string `json:"headerSHA256"`
	Objects        []objectRecord    `json:"objects"`
	Loaded         bool              `json:"loaded"`
	Approval       string            `json:"approval"`
	ISA            string            `json:"isa"`
}

type buildInputs struct {
	record  []byte
	objects map[filecache.ArtifactID][]byte
	sources map[string][]byte
	patch   []byte
}

func sha(data []byte) string { value := sha256.Sum256(data); return hex.EncodeToString(value[:]) }

func readInputs(root, patchPath string) (buildInputs, error) {
	var input buildInputs
	data, err := readBounded(filepath.Join(root, "build.json"), 65536)
	if err != nil {
		return input, err
	}
	var record buildRecord
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&record) != nil {
		return input, errBuild
	}
	canonical, err := json.Marshal(record)
	var compact bytes.Buffer
	if err != nil || json.Compact(&compact, data) != nil || !bytes.Equal(canonical, compact.Bytes()) ||
		record.Builder != "ghcr.io/inspektor-gadget/gadget-builder@"+filecache.BuilderDigest || record.UpstreamCommit != filecache.EngineSourceCommit ||
		record.Loaded || record.Approval != "not granted by this build" || record.ISA != "bpfel v3" ||
		len(record.SourceSHA256) != 4 || len(record.HeaderSHA256) == 0 || len(record.Objects) != 4 {
		return input, errBuild
	}
	input.record = data
	input.sources = make(map[string][]byte)
	for _, name := range []string{"LICENSE", "files.bpf.c", "cache.bpf.c", "common.h"} {
		data, err := readBounded(filepath.Join(root, "source", name), 65536)
		if err != nil || sha(data) != record.SourceSHA256[name] {
			return input, errBuild
		}
		input.sources[name] = data
	}
	input.objects = make(map[filecache.ArtifactID][]byte)
	for _, object := range record.Objects {
		id := filecache.ArtifactID{Kind: object.Kind, Architecture: object.Architecture}
		if (id.Kind != trace.Files && id.Kind != trace.Cache) || (id.Architecture != "arm64" && id.Architecture != "amd64") || input.objects[id] != nil || !object.Reproduced {
			return input, errBuild
		}
		name := string(id.Kind) + "-" + id.Architecture
		if object.Path != name+"-1/program.bpf.o" {
			return input, errBuild
		}
		first, err := readBounded(filepath.Join(root, object.Path), filecache.MaxObjectBytes)
		if err != nil || sha(first) != object.SHA256 {
			return input, errBuild
		}
		second, err := readBounded(filepath.Join(root, name+"-2/program.bpf.o"), filecache.MaxObjectBytes)
		if err != nil || !bytes.Equal(first, second) {
			return input, errBuild
		}
		input.objects[id] = first
	}
	input.patch, err = readBounded(patchPath, 65536)
	if err != nil || sha(input.patch) != filecache.EnginePatchSHA256 {
		return input, errBuild
	}
	return input, nil
}
