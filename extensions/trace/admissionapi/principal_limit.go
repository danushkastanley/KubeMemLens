package admissionapi

import (
	"sync"

	"golang.org/x/time/rate"
)

// Each principal may send five requests a second with a burst of ten. That is
// a quarter of the shared budget, so one caller in a loop cannot use it up.
const (
	principalRate       = 5
	principalBurst      = 10
	maxTrackedPrincipal = 1024
)

// principalLimits gives each authenticated principal its own request budget.
// It is checked before the shared limiter, so a refused request spends none
// of the budget that other principals share.
type principalLimits struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
}

func newPrincipalLimits() *principalLimits {
	return &principalLimits{limiters: map[string]*rate.Limiter{}}
}

func (p *principalLimits) allow(name string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	limiter := p.limiters[name]
	if limiter == nil {
		if len(p.limiters) >= maxTrackedPrincipal {
			// Bounded memory. Every principal starts a fresh burst; the shared
			// limiter still caps the total.
			clear(p.limiters)
		}
		limiter = rate.NewLimiter(principalRate, principalBurst)
		p.limiters[name] = limiter
	}
	return limiter.Allow()
}
