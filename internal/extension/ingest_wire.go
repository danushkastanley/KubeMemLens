package extension

import (
	"errors"
	"net/http"

	"github.com/danushkastanley/kube-memlens/internal/collector"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
)

// boundIngestWire bounds the request on its wire form, before the shared
// typed decode can allocate. Each producer role has its own envelope: the
// Node-context producer may send no containers, and the cgroup producer may
// send at most the configured container limit.
func (h *Handler) boundIngestWire(role ProducerRole, body []byte) *IngestionError {
	if role == NodeContextProducer {
		if boundedNodeWireWithVolumes(body, volumecontext.MaxBatchRecords) != nil {
			return reject(http.StatusBadRequest, "invalid_json", "invalid bounded Node-only snapshot JSON", "invalid_json")
		}
		return nil
	}
	err := collector.BoundSnapshotWire(body, h.coordinator.opts.Handler.MaxContainers)
	if errors.Is(err, collector.ErrSnapshotContainerLimit) {
		return reject(http.StatusBadRequest, "invalid_snapshot", err.Error(), "invalid_snapshot")
	}
	if err != nil {
		return reject(http.StatusBadRequest, "invalid_json", "invalid snapshot JSON", "invalid_json")
	}
	return nil
}
