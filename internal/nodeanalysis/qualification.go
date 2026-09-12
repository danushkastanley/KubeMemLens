package nodeanalysis

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

const ChargeInclusive = "cgroup-charge-inclusive"

// Qualification is operator-owned evidence, never inferred from a matching
// value or accepted from a producer/read request. Bind it to this Node boot.
type Qualification struct {
	NodeUID         string                       `json:"nodeUID"`
	NodeStartedAt   time.Time                    `json:"nodeStartedAt"`
	ValidFrom       time.Time                    `json:"validFrom"`
	ExpiresAt       time.Time                    `json:"expiresAt"`
	EvidenceSHA256  string                       `json:"evidenceSHA256"`
	UsageDefinition string                       `json:"usageDefinition"`
	DisjointSystems []nodecontext.SystemCategory `json:"disjointSystems,omitempty"`
}

func (q Qualification) Validate() error {
	digest, err := hex.DecodeString(q.EvidenceSHA256)
	if err != nil || len(digest) != 32 || q.EvidenceSHA256 != strings.ToLower(q.EvidenceSHA256) ||
		q.NodeUID == "" || len(q.NodeUID) > nodecontext.MaxNodeUIDBytes || q.NodeStartedAt.IsZero() ||
		q.ValidFrom.IsZero() || !q.ExpiresAt.After(q.ValidFrom) || q.ExpiresAt.Sub(q.ValidFrom) > 7*24*time.Hour || q.UsageDefinition != ChargeInclusive {
		return fmt.Errorf("invalid bounded Node accounting qualification")
	}
	seen := map[nodecontext.SystemCategory]bool{}
	for _, category := range q.DisjointSystems {
		switch category {
		case nodecontext.Kubelet, nodecontext.Runtime, nodecontext.Misc:
		default:
			return fmt.Errorf("system category is not eligible for disjoint accounting")
		}
		if seen[category] {
			return fmt.Errorf("duplicate disjoint system category")
		}
		seen[category] = true
	}
	return nil
}

func qualified(input Input) bool {
	q := input.Qualification
	return q != nil && q.Validate() == nil && input.Current != nil && input.Current.Stats != nil &&
		q.NodeUID == input.NodeUID && q.NodeStartedAt.Equal(input.Current.Stats.StartedAt) &&
		!input.Now.Before(q.ValidFrom) && input.Now.Before(q.ExpiresAt)
}
