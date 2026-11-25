package nodebinding

import (
	"time"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	"github.com/danushkastanley/kube-memlens/internal/traceframe"
)

// grantedUntil turns an instant from the controller's clock into the one this
// node honours. limit is the longest the node grants from now. A controller
// instant beyond it means this node's clock is behind: within the allowance
// the node grants its own limit, so nothing lasts longer than admitted; beyond
// it the clocks disagree too much to trust and the operator is told why.
func (s *Service) grantedUntil(requested, now time.Time, limit time.Duration) (time.Time, error) {
	latest := now.Add(limit)
	switch {
	case !requested.After(now):
		return time.Time{}, admission.ErrExpired
	case requested.After(latest.Add(traceframe.ClockSkewAllowance)):
		s.audit("clock_skew")
		return time.Time{}, admission.ErrUnavailable
	case requested.After(latest):
		return latest, nil
	}
	return requested, nil
}
