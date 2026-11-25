package extension

import (
	"time"

	"golang.org/x/time/rate"
)

// producerKey separates the producers on one Node. The cgroup agent and the
// Node-context producer report the same Node UID, so the UID alone would make
// them share one allowance.
type producerKey struct {
	role    ProducerRole
	nodeUID string
}

// defaultBodyReadTimeout bounds how long one snapshot body may take to arrive.
// A full-size body crosses the cluster network in well under a second.
const defaultBodyReadTimeout = 5 * time.Second

// beginProducer admits a snapshot only when no earlier one from the same
// producer is still being received. The entry is removed by endProducer, so
// the map holds at most one key per request in progress.
func (h *Handler) beginProducer(claims AgentClaims) bool {
	key := producerKey{role: claims.Role, nodeUID: claims.NodeUID}
	h.limiterMu.Lock()
	defer h.limiterMu.Unlock()
	if _, busy := h.inFlight[key]; busy {
		return false
	}
	h.inFlight[key] = struct{}{}
	return true
}

func (h *Handler) endProducer(claims AgentClaims) {
	h.limiterMu.Lock()
	defer h.limiterMu.Unlock()
	delete(h.inFlight, producerKey{role: claims.Role, nodeUID: claims.NodeUID})
}

type identityLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func (h *Handler) allowProducer(claims AgentClaims, now time.Time) bool {
	key := producerKey{role: claims.Role, nodeUID: claims.NodeUID}
	h.limiterMu.Lock()
	defer h.limiterMu.Unlock()
	entry, exists := h.limiters[key]
	if !exists {
		cutoff := now.Add(-h.opts.IdentityTTL)
		for candidateKey, candidate := range h.limiters {
			if candidate.lastSeen.Before(cutoff) {
				delete(h.limiters, candidateKey)
			}
		}
		if len(h.limiters) >= h.opts.MaxIdentities {
			var oldestKey producerKey
			oldestSeen := now
			found := false
			for candidateKey, candidate := range h.limiters {
				if !found || candidate.lastSeen.Before(oldestSeen) {
					oldestKey = candidateKey
					oldestSeen = candidate.lastSeen
					found = true
				}
			}
			delete(h.limiters, oldestKey)
		}
		entry = identityLimiter{limiter: rate.NewLimiter(rate.Limit(h.opts.RequestsPerSec), h.opts.Burst)}
	}
	entry.lastSeen = now
	h.limiters[key] = entry
	return entry.limiter.Allow()
}
