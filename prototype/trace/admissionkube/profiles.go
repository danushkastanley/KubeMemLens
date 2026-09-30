package admissionkube

import (
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/prototype/trace/nodeprofile"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
)

// NewPinnedResolver retains its own immutable profile values. The existing
// fresh Node read checks them during admission, stream revalidation and OOM
// context acquisition, without widening RBAC or adding requests.
func NewPinnedResolver(core coreclient.CoreV1Interface, profiles []nodeprofile.Profile) (*Resolver, error) {
	if core == nil || len(profiles) == 0 || len(profiles) > 64 {
		return nil, admission.ErrInvalidRequest
	}
	byUID := make(map[string]nodeprofile.Profile, len(profiles))
	names := map[string]bool{}
	for _, profile := range profiles {
		if profile.Validate() != nil || names[profile.NodeName] {
			return nil, admission.ErrInvalidRequest
		}
		if _, exists := byUID[profile.NodeUID]; exists {
			return nil, admission.ErrInvalidRequest
		}
		byUID[profile.NodeUID] = profile
		names[profile.NodeName] = true
	}
	return &Resolver{core: core, profiles: byUID}, nil
}
