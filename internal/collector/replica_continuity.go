package collector

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"github.com/danushkastanley/kube-memlens/internal/replicabaseline"
)

// Retain one fingerprint and continuity clock per existing bounded history
// series. Changes to contributors or enforcement must not resemble growth.
type replicaContinuity struct {
	fingerprint string
	since       time.Time
}

type replicaEnforcement struct {
	Name                                                                      string
	Min, Low, High, Max, SwapMax                                              uint64
	MinKnown, LowKnown, HighKnown, MaxKnown, SwapMaxKnown                     bool
	MinUnlimited, LowUnlimited, HighUnlimited, MaxUnlimited, SwapMaxUnlimited bool
}

func enforcement(c api.ContainerSnapshot) replicaEnforcement {
	m := c.Memory
	return replicaEnforcement{Name: c.ContainerName, Min: m.MinBytes, Low: m.LowBytes, High: m.HighBytes, Max: m.MaxBytes, SwapMax: m.SwapMaxBytes,
		MinKnown: m.MinKnown, LowKnown: m.LowKnown, HighKnown: m.HighKnown, MaxKnown: m.MaxKnown, SwapMaxKnown: m.SwapMaxKnown,
		MinUnlimited: m.MinUnlimited, LowUnlimited: m.LowUnlimited, HighUnlimited: m.HighUnlimited, MaxUnlimited: m.MaxUnlimited, SwapMaxUnlimited: m.SwapMaxUnlimited}
}

func replicaFingerprint(nodeUID string, containers []api.ContainerSnapshot) string {
	if nodeUID == "" || len(containers) == 0 || len(containers) > replicabaseline.MaxContainers {
		return ""
	}
	type entry struct {
		Name, ID, Path string
		Configured     model.MemoryResourceBudget
		Resources      model.ContainerMemoryResources
		Enforcement    replicaEnforcement
	}
	items := make([]entry, 0, len(containers))
	seen := map[string]bool{}
	for _, c := range containers {
		if c.ContainerName == "" || c.ContainerID == "" || c.Completeness == api.EvidencePartial || seen[c.ContainerName] {
			return ""
		}
		seen[c.ContainerName] = true
		configured := model.MemoryResourceBudget{Request: model.ResourceValue{Bytes: c.Context.MemoryRequestBytes, Known: c.Context.MemoryRequestKnown}, Limit: model.ResourceValue{Bytes: c.Context.MemoryLimitBytes, Known: c.Context.MemoryLimitKnown}}
		items = append(items, entry{c.ContainerName, c.ContainerID, c.CgroupPath, configured, c.Context.Resources, enforcement(c)})
	}
	slices.SortFunc(items, func(a, b entry) int { return strings.Compare(a.Name, b.Name) })
	return replicaHash(struct {
		NodeUID    string
		Containers []entry
	}{nodeUID, items})
}

func (c *replicaContinuity) record(at time.Time, fingerprint string, previous time.Time, gap time.Duration) {
	if fingerprint == "" {
		*c = replicaContinuity{}
		return
	}
	if fingerprint != c.fingerprint || c.since.IsZero() || at.Sub(previous) > gap {
		c.since = at
	}
	c.fingerprint = fingerprint
}

func replicaMeasuredShape(metadata string, containers []api.ContainerSnapshot) string {
	if metadata == "" {
		return ""
	}
	items := make([]replicaEnforcement, 0, len(containers))
	for _, c := range containers {
		items = append(items, enforcement(c))
	}
	slices.SortFunc(items, func(a, b replicaEnforcement) int { return strings.Compare(a.Name, b.Name) })
	return replicaHash(struct {
		Metadata    string
		Enforcement []replicaEnforcement
	}{metadata, items})
}

func replicaHash(value any) string {
	encoded, _ := json.Marshal(value)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
