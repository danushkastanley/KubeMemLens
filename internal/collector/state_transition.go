package collector

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

// expirySpan records when stored node evidence passed its TTL.
type expirySpan struct {
	first time.Time
	last  time.Time
}

func (e *expirySpan) add(expiredAt time.Time) {
	if e.first.IsZero() || expiredAt.Before(e.first) {
		e.first = expiredAt
	}
	if expiredAt.After(e.last) {
		e.last = expiredAt
	}
}

// expiryTransition returns when TTL expiry moved the collector into state,
// and false when something else did. The state is recomputed only on reads,
// yet evidence expires on its own between them, so an expiry is dated by the
// evidence rather than by whichever read noticed it. A change caused by a
// write has no such record and is dated by the read that saw it.
func expiryTransition(previous, state api.CollectorState, staleOnly bool, expired expirySpan) (time.Time, bool) {
	switch {
	case state == api.CollectorStale:
		// Every node is stale; the state began when the last one expired.
		return expired.last, true
	case state == api.CollectorDegraded && previous == api.CollectorReady && staleOnly:
		return expired.first, true
	}
	return time.Time{}, false
}

// transitionTime dates a state change seen at now. An expiry that predates the
// previous transition means a write happened in between, so the earlier
// transition bounds it.
func transitionTime(previous, state api.CollectorState, staleOnly bool, expired expirySpan, since, now time.Time) time.Time {
	expiredAt, ok := expiryTransition(previous, state, staleOnly, expired)
	if !ok {
		return now
	}
	if expiredAt.Before(since) {
		return since
	}
	return expiredAt
}
