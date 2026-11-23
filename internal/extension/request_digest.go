package extension

import (
	"crypto/sha256"
	"encoding/json"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

type requestDigest struct {
	// content identifies the snapshot alone, so the same snapshot sent again
	// under a new sequence can be told apart from a different one.
	content [sha256.Size]byte
	// request identifies the whole request, sequence included.
	request [sha256.Size]byte
}

// digestRequest serialises the snapshot once. The request digest covers every
// other request field through the envelope, then the snapshot through its
// digest.
func digestRequest(request api.NodeSnapshotRequest) (requestDigest, error) {
	snapshot, err := json.Marshal(request.Snapshot)
	if err != nil {
		return requestDigest{}, err
	}
	content := sha256.Sum256(snapshot)
	request.Snapshot = api.AgentSnapshot{}
	envelope, err := json.Marshal(request)
	if err != nil {
		return requestDigest{}, err
	}
	return requestDigest{content: content, request: sha256.Sum256(append(envelope, content[:]...))}, nil
}
