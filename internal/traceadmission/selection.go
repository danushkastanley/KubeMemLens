package traceadmission

import (
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/tracecompat"

	"k8s.io/apimachinery/pkg/util/validation"
)

// selectedLifetime is a compare-only precondition. It cannot supply a Node UID,
// cgroup or any other binding authority. Admission still resolves the target and
// obtains its cgroup from the authenticated node service.
type selectedLifetime struct {
	podUID, containerID, nodeName string
	startedAt                     time.Time
}

func decodeSelection(b requestBody) (*selectedLifetime, error) {
	if b.SchemaVersion == tracecompat.RequestSchema {
		if b.ContractVersion == nil || *b.ContractVersion != uint16(tracecompat.Current) {
			return nil, ErrInvalidRequest
		}
	} else if b.ContractVersion != nil {
		return nil, ErrInvalidRequest
	}
	hasSelection := b.ExpectedPodUID != nil || b.ExpectedContainerID != nil || b.ExpectedContainerStartedAt != nil || b.ExpectedNodeName != nil
	if b.SchemaVersion == 1 && !hasSelection {
		return nil, nil
	}
	if (b.SchemaVersion != 2 && b.SchemaVersion != tracecompat.RequestSchema) || b.ExpectedPodUID == nil || b.ExpectedContainerID == nil || b.ExpectedContainerStartedAt == nil || b.ExpectedNodeName == nil {
		return nil, ErrInvalidRequest
	}
	s := selectedLifetime{podUID: *b.ExpectedPodUID, containerID: *b.ExpectedContainerID,
		nodeName: *b.ExpectedNodeName, startedAt: b.ExpectedContainerStartedAt.UTC()}
	if len(s.podUID) == 0 || len(s.podUID) > 128 ||
		strings.Trim(s.podUID, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_.") != "" ||
		len(s.containerID) != 64 || strings.Trim(s.containerID, "0123456789abcdef") != "" ||
		s.startedAt.IsZero() || len(validation.IsDNS1123Subdomain(s.nodeName)) != 0 {
		return nil, ErrInvalidRequest
	}
	for _, label := range strings.Split(s.nodeName, ".") {
		if len(label) > 63 {
			return nil, ErrInvalidRequest
		}
	}
	return &s, nil
}

func (r Request) matchesSelection(w Workload) bool {
	if r.selection == nil {
		return true // Version 1 keeps its server-resolved name-based contract.
	}
	s, t := r.selection, w.Target
	return s.podUID == t.PodUID && s.containerID == t.ContainerID &&
		s.startedAt.Equal(t.ContainerStartedAt) && s.nodeName == w.NodeName
}
