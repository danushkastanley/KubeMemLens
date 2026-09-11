package nodeanalysis

import (
	"fmt"
	"slices"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

func Analyse(input Input) (Analysis, error) {
	if input.Now.IsZero() || input.NodeName == "" || input.NodeUID == "" {
		return Analysis{}, fmt.Errorf("Node identity and evaluation time are required")
	}
	if input.Access != ClusterPods {
		input.Cgroup = CgroupFrame{}
	}
	if input.Rank == "" {
		input.Rank = Total
	}
	switch input.Rank {
	case Total, Anon, Cache, Shmem, Residual, PSI, OOM:
	default:
		return Analysis{}, fmt.Errorf("unsupported contributor ranking")
	}
	if input.Limit == 0 {
		input.Limit = DefaultContributors
	}
	if input.Limit < 1 || input.Limit > MaxContributors || len(input.Cgroup.Containers) > MaxContainers {
		return Analysis{}, fmt.Errorf("Node analysis exceeds input or ranking bounds")
	}
	if input.Access != "" && input.Access != NodeOnly && input.Access != ClusterPods {
		return Analysis{}, fmt.Errorf("unsupported contributor scope")
	}
	if err := validateInput(input); err != nil {
		return Analysis{}, err
	}
	result := Analysis{SchemaVersion: SchemaVersion, NodeName: input.NodeName, NodeUID: input.NodeUID, EvaluatedAt: input.Now,
		Severity: Unknown, Confidence: Low, Signals: []Signal{}, Caveats: []Caveat{}, ContributorAccess: NodeOnly,
		Facts:       Facts{Source: nodecontext.Source, Availability: input.SourceAvailability},
		OutsidePods: unavailableEstimate("max(0, node.usage - observedPodCharge)"),
		Unaccounted: unavailableEstimate("max(0, node.usage - observedPodCharge - qualifiedDisjointSystemUsage)")}
	current := input.Current
	if current != nil && (current.NodeUID != input.NodeUID || current.NodeName != input.NodeName) {
		current = nil
		addCaveat(&result, IdentityMismatch)
	}
	input.Current = current
	if current == nil {
		addCaveat(&result, SourceMissing)
	} else {
		result.Confidence = Medium
		result.Facts.Context = cloneContext(current.Context)
		if current.Context != nil {
			result.Facts.ContextSource = capability.KubernetesStatus
		}
		if current.Stats != nil {
			result.Facts.Memory = cloneMemory(current.Stats.Memory)
			result.Facts.Swap = cloneSwap(current.Stats.Swap)
			result.Facts.SystemContainers = cloneSystems(current.Stats.SystemContainers)
		}
		if current.Context != nil && len(current.Context.Hugepages) > 0 {
			addCaveat(&result, HugepagesSeparate)
		}
		if !fresh(current.Evidence.CapturedAt, input.Now, nodecontext.StaleAfter) {
			addCaveat(&result, SourceStale)
			result.Confidence = Low
		}
		if input.SourceAvailability != capability.Available {
			addCaveat(&result, SourceFailed)
			result.Confidence = Low
		}
		deriveRates(input, &result)
		pressure(input, &result)
	}
	if input.Access == ClusterPods {
		result.ContributorAccess = ClusterPods
		aggregateContributors(input, &result)
		estimates(input, &result)
	} else {
		addCaveat(&result, PodAccessDenied)
		result.OutsidePods.Caveats = []Caveat{PodAccessDenied}
		result.Unaccounted.Caveats = []Caveat{PodAccessDenied}
	}
	if result.Severity == Unknown {
		addCaveat(&result, NoPressureEvidence)
	}
	slices.Sort(result.Caveats)
	return result, nil
}

func unavailableEstimate(formula string) Estimate {
	return Estimate{State: capability.Unreported, Formula: formula, Source: "kubelet-summary + cgroup", Caveats: []Caveat{}}
}

func addCaveat(result *Analysis, value Caveat) {
	if !slices.Contains(result.Caveats, value) {
		result.Caveats = append(result.Caveats, value)
	}
}

func fresh(at, now time.Time, ttl time.Duration) bool {
	return !at.IsZero() && !at.Before(now.Add(-ttl)) && !at.After(now.Add(30*time.Second))
}

func aligned(left, right time.Time) bool {
	difference := left.Sub(right)
	return difference >= -nodecontext.MaxSampleSkew && difference <= nodecontext.MaxSampleSkew
}
