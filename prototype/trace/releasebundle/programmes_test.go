package releasebundle

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
)

func candidateIndex(key []byte) filecache.CandidateIndex {
	index := filecache.CandidateIndex{Version: 2, Status: "unapproved", PublicKeySHA256: sum(key)}
	for _, arch := range []string{"amd64", "arm64"} {
		for _, kind := range []trace.Kind{trace.Files, trace.Cache, trace.OOM} {
			index.Programmes = append(index.Programmes, filecache.CandidateEntry{Kind: kind, Architecture: arch, ManifestSHA256: sum([]byte(string(kind) + arch))})
		}
	}
	return index
}
func TestProgrammeIndexIsCompleteAndDoesNotBecomeApproval(t *testing.T) {
	key := bytes.Repeat([]byte{1}, 32)
	index := candidateIndex(key)
	raw, _ := json.Marshal(index)
	if _, err := programmeIndex(raw, key); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*filecache.CandidateIndex){
		func(i *filecache.CandidateIndex) { i.Status = "approved" },
		func(i *filecache.CandidateIndex) { i.Version = 1 },
		func(i *filecache.CandidateIndex) { i.Programmes = i.Programmes[:5] },
		func(i *filecache.CandidateIndex) { i.Programmes[1] = i.Programmes[0] },
		func(i *filecache.CandidateIndex) { i.Programmes[0].Architecture = "other" },
		func(i *filecache.CandidateIndex) { i.Programmes[0].Kind = "other" },
		func(i *filecache.CandidateIndex) { i.PublicKeySHA256 = strings.Repeat("f", 64) },
	} {
		value := candidateIndex(key)
		change(&value)
		data, _ := json.Marshal(value)
		if _, err := programmeIndex(data, key); !errors.Is(err, ErrArchive) {
			t.Fatal("unsafe index accepted")
		}
	}
}
func TestProgrammeSignatureBindsKindArchitectureBuildAndReviewedToolchain(t *testing.T) {
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{3}, 32))
	key := private.Public().(ed25519.PublicKey)
	id := filecache.ArtifactID{Kind: trace.Files, Architecture: "amd64"}
	build := strings.Repeat("b", 64)
	manifest := filecache.Manifest{Version: 1, Kind: id.Kind, Architecture: id.Architecture, ObjectSHA256: strings.Repeat("a", 64), SourceSHA256: build, OCIManifestSHA256: strings.Repeat("c", 64), EngineCommit: filecache.EngineSourceCommit, BuilderDigest: filecache.BuilderDigest, EnginePatchSHA256: filecache.EnginePatchSHA256}
	raw, _ := json.Marshal(manifest)
	signature := ed25519.Sign(private, raw)
	if _, err := programmeManifest(id, raw, signature, key, sum(raw), build); err != nil {
		t.Fatal(err)
	}
	if _, err := programmeManifest(id, raw, bytes.Repeat([]byte{1}, 64), key, sum(raw), build); !errors.Is(err, ErrArchive) {
		t.Fatal("wrong signature accepted")
	}
	for _, change := range []func(*filecache.Manifest){
		func(m *filecache.Manifest) { m.Kind = trace.Cache }, func(m *filecache.Manifest) { m.Architecture = "arm64" },
		func(m *filecache.Manifest) { m.BuilderDigest = "sha256:" + strings.Repeat("d", 64) },
		func(m *filecache.Manifest) { m.EngineCommit = strings.Repeat("d", 40) },
		func(m *filecache.Manifest) { m.EnginePatchSHA256 = strings.Repeat("d", 64) },
		func(m *filecache.Manifest) { m.SourceSHA256 = strings.Repeat("d", 64) },
	} {
		m := manifest
		change(&m)
		data, _ := json.Marshal(m)
		if _, err := programmeManifest(id, data, ed25519.Sign(private, data), key, sum(data), build); !errors.Is(err, ErrArchive) {
			t.Fatal("valid signature bypassed programme identity")
		}
	}
}
func TestProgrammeBuildRecordRetainsSourceAndHeaderBindings(t *testing.T) {
	files := FileSet{entries: map[string]Entry{}}
	record := programmeBuild{Builder: "ghcr.io/inspektor-gadget/gadget-builder@" + filecache.BuilderDigest, UpstreamCommit: filecache.EngineSourceCommit, SourceSHA256: map[string]string{}, HeaderSHA256: map[string]string{"include/gadget/header.h": strings.Repeat("b", 64)}, Approval: "not granted by this build", ISA: "bpfel v3"}
	for _, name := range []string{"LICENSE", "files.bpf.c", "cache.bpf.c", "oom.bpf.c", "common.h"} {
		digest := sum([]byte(name))
		record.SourceSHA256[name] = digest
		files.entries["source/"+name] = Entry{Name: "source/" + name, Type: Regular, SHA256: digest}
	}
	for _, arch := range []string{"amd64", "arm64"} {
		for _, kind := range []trace.Kind{trace.Files, trace.Cache, trace.OOM} {
			record.Objects = append(record.Objects, programmeObject{Kind: kind, Architecture: arch, SHA256: sum([]byte(string(kind) + arch)), Path: string(kind) + "-" + arch + "-1/program.bpf.o", Reproduced: true})
		}
	}
	raw, _ := json.Marshal(record)
	actual, err := programmeBuildRecord(raw, files)
	if err != nil || len(actual.objects) != 6 || actual.headers["sources/upstream-headers/gadget/header.h"] != strings.Repeat("b", 64) {
		t.Fatal("valid build record refused", err)
	}
	for _, change := range []func(*programmeBuild){
		func(r *programmeBuild) { r.Loaded = true }, func(r *programmeBuild) { r.Approval = "approved" },
		func(r *programmeBuild) { r.Objects[0].Reproduced = false }, func(r *programmeBuild) { r.Objects[0].Path = "../escape" },
		func(r *programmeBuild) { r.Objects[1] = r.Objects[0] }, func(r *programmeBuild) { r.HeaderSHA256 = map[string]string{"../escape": strings.Repeat("a", 64)} },
	} {
		var value programmeBuild
		_ = json.Unmarshal(raw, &value)
		change(&value)
		data, _ := json.Marshal(value)
		if _, err := programmeBuildRecord(data, files); !errors.Is(err, ErrArchive) {
			t.Fatal("inconsistent build record accepted")
		}
	}
}
func TestProgrammeImageCopiesAndCompilerHeadersMustMatch(t *testing.T) {
	name := "candidate-index.json"
	entry := Entry{Name: name, Type: Regular, Size: 1, Mode: 0644, SHA256: strings.Repeat("a", 64)}
	programmes := Programmes{digest: "sha256:" + strings.Repeat("b", 64), files: FileSet{entries: map[string]Entry{name: entry}}, headers: map[string]string{"sources/upstream-headers/gadget/header.h": strings.Repeat("c", 64)}}
	engine := Engine{digest: "sha256:" + strings.Repeat("d", 64), files: FileSet{entries: map[string]Entry{"sources/upstream-headers/gadget/header.h": {Type: Regular, SHA256: strings.Repeat("c", 64)}}}}
	image := Image{platforms: map[string]FileSet{}}
	for _, arch := range []string{"amd64", "arm64"} {
		image.platforms[arch] = FileSet{entries: map[string]Entry{programmeImagePath(name): entry}}
	}
	if MatchProgrammes(image, engine, programmes) != nil {
		t.Fatal("matching image refused")
	}
	changed := entry
	changed.SHA256 = strings.Repeat("e", 64)
	image.platforms["arm64"].entries[programmeImagePath(name)] = changed
	if MatchProgrammes(image, engine, programmes) == nil {
		t.Fatal("altered programme copy accepted")
	}
	image.platforms["arm64"].entries[programmeImagePath(name)] = entry
	delete(engine.files.entries, "sources/upstream-headers/gadget/header.h")
	if MatchProgrammes(image, engine, programmes) == nil {
		t.Fatal("missing compiler header accepted")
	}
}
