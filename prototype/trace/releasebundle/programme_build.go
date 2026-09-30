package releasebundle

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
)

type programmeBuild struct {
	Builder        string            `json:"builder"`
	UpstreamCommit string            `json:"upstreamCommit"`
	SourceSHA256   map[string]string `json:"sourceSHA256"`
	HeaderSHA256   map[string]string `json:"headerSHA256"`
	Objects        []programmeObject `json:"objects"`
	Loaded         bool              `json:"loaded"`
	Approval       string            `json:"approval"`
	ISA            string            `json:"isa"`
}
type programmeObject struct {
	Kind         trace.Kind `json:"kind"`
	Architecture string     `json:"architecture"`
	SHA256       string     `json:"sha256"`
	Path         string     `json:"path"`
	Reproduced   bool       `json:"reproduced"`
}
type programmeInputs struct {
	objects map[filecache.ArtifactID]string
	headers map[string]string
}

func programmeBuildRecord(data []byte, files FileSet) (programmeInputs, error) {
	var record programmeBuild
	if len(data) > 65536 || decodeOCI(data, &record) != nil {
		return programmeInputs{}, ErrArchive
	}
	canonical, err := json.Marshal(record)
	var compact bytes.Buffer
	if err != nil || json.Compact(&compact, data) != nil || !bytes.Equal(canonical, compact.Bytes()) || record.Builder != "ghcr.io/inspektor-gadget/gadget-builder@"+filecache.BuilderDigest || record.UpstreamCommit != filecache.EngineSourceCommit || record.Loaded || record.Approval != "not granted by this build" || record.ISA != "bpfel v3" || len(record.Objects) != 6 || len(record.SourceSHA256) != 5 || len(record.HeaderSHA256) == 0 || len(record.HeaderSHA256) > 128 {
		return programmeInputs{}, ErrArchive
	}
	for _, name := range []string{"LICENSE", "files.bpf.c", "cache.bpf.c", "oom.bpf.c", "common.h"} {
		entry, found := files.Entry("source/" + name)
		if !found || entry.Type != Regular || !shaPattern.MatchString(record.SourceSHA256[name]) || entry.SHA256 != record.SourceSHA256[name] {
			return programmeInputs{}, ErrArchive
		}
	}
	objects := make(map[filecache.ArtifactID]string)
	for _, object := range record.Objects {
		id := filecache.ArtifactID{Kind: object.Kind, Architecture: object.Architecture}
		if !validProgrammeID(id) || objects[id] != "" || !object.Reproduced || !shaPattern.MatchString(object.SHA256) || object.Path != string(object.Kind)+"-"+object.Architecture+"-1/program.bpf.o" {
			return programmeInputs{}, ErrArchive
		}
		objects[id] = object.SHA256
	}
	headers := make(map[string]string)
	for name, digest := range record.HeaderSHA256 {
		if !canonicalPath(name) || !strings.HasPrefix(name, "include/gadget/") || !shaPattern.MatchString(digest) {
			return programmeInputs{}, ErrArchive
		}
		headers["sources/upstream-headers/"+strings.TrimPrefix(name, "include/")] = digest
	}
	return programmeInputs{objects, headers}, nil
}
