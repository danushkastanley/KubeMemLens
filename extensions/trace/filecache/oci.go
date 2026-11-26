package filecache

import "encoding/json"

const OCIManifestMediaType = "application/vnd.oci.image.manifest.v1+json"
const ProgrammeArtifactType = "application/vnd.kubememlens.filecache.v1"
const ProgrammeMediaType = "application/vnd.gadget.ebpf.program.v1+binary"
const emptyJSONDigest = "sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"

type descriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int    `json:"size"`
}
type ociManifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	MediaType     string            `json:"mediaType"`
	ArtifactType  string            `json:"artifactType"`
	Config        descriptor        `json:"config"`
	Layers        []descriptor      `json:"layers"`
	Annotations   map[string]string `json:"annotations"`
}

// OCIManifest constructs the one permitted OCI 1.1 artifact shape: empty JSON
// config and one uncompressed BPF layer. It neither validates nor approves ELF.
// No URLs, embedded data, arbitrary layers or registry fallback are permitted.
func OCIManifest(id ArtifactID, object []byte) ([]byte, error) {
	if !validArtifactID(id) || len(object) == 0 || len(object) > MaxObjectBytes {
		return nil, ErrArtifact
	}
	return json.Marshal(ociManifest{
		SchemaVersion: 2, MediaType: OCIManifestMediaType, ArtifactType: ProgrammeArtifactType,
		Config:      descriptor{"application/vnd.oci.empty.v1+json", emptyJSONDigest, 2},
		Layers:      []descriptor{{ProgrammeMediaType, "sha256:" + sumSHA(object), len(object)}},
		Annotations: map[string]string{"io.kubememlens.architecture": id.Architecture, "io.kubememlens.kind": string(id.Kind)},
	})
}
