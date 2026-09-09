package cli

import (
	"fmt"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/explain"
)

func (r *doctorReport) addMemoryQoSCheck(containers []api.ContainerSnapshot) {
	unknown, stale, unsettled, inconsistent, crossed, finite := 0, 0, 0, 0, 0, 0
	for _, container := range containers {
		qos := explain.InterpretMemoryQoS(container)
		switch qos.State {
		case explain.QoSUnavailable:
			unknown++
		case explain.QoSStale:
			stale++
		case explain.QoSResizing:
			unsettled++
		case explain.QoSInconsistent:
			inconsistent++
		}
		if qos.High.State == explain.BoundaryFinite || qos.High.State == explain.BoundaryZero {
			finite++
		}
		if qos.Activity == explain.ThrottleCrossed || qos.Activity == explain.ThrottleCrossedWithStalls {
			crossed++
		}
	}
	status := "pass"
	if len(containers) == 0 || unknown+stale+unsettled+inconsistent+crossed > 0 {
		status = "warn"
	}
	r.addCheck("MemoryQoS observations", status, fmt.Sprintf("%d containers; finite leaf high=%d, recent crossings=%d, unavailable=%d, stale=%d, resizing=%d, inconsistent=%d; unlimited high alone is ordinary, parent controls and kubelet policy are unobserved", len(containers), finite, crossed, unknown, stale, unsettled, inconsistent))
}
