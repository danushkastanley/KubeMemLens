package cli

import (
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

func TestCaptureReliabilityPreservesPartialFreshEvidence(t *testing.T) {
	result := captureReliabilityFromPods([]api.PodSnapshot{{
		Freshness: api.EvidenceFreshnessFresh, Completeness: api.EvidencePartial,
	}})
	if result.State != api.CollectorDegraded || result.Completeness != api.EvidencePartial {
		t.Fatalf("capture reliability = %#v", result)
	}
}
