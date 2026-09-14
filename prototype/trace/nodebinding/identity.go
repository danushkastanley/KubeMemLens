package nodebinding

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
)

const controllerHeader = "X-Memlens-Control-Instance"
const nodeHeader = "X-Memlens-Node-Instance"

type identityResponse struct {
	NodeUID  string `json:"nodeUID"`
	Instance string `json:"instance"`
}

func newInstance() (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", admission.ErrUnavailable
	}
	return hex.EncodeToString(nonce[:]), nil
}
func (n nodeClient) identity(ctx context.Context, uid string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, n.url+"/v1/identity", nil)
	if err != nil {
		return "", admission.ErrUnavailable
	}
	request.Header.Set(controllerHeader, n.owner)
	response, err := n.http.Do(request)
	if err != nil {
		return "", admission.ErrUnavailable
	}
	defer response.Body.Close()
	var identity identityResponse
	if response.StatusCode != 200 || decode(response.Body, &identity, "nodeUID|instance") != nil || identity.NodeUID != uid || !validID(identity.Instance) {
		return "", admission.ErrUnavailable
	}
	return identity.Instance, nil
}
func (s *Service) writeIdentity(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(identityResponse{s.nodeUID, s.instance})
}
