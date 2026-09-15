package workerprocess

import (
	"context"
	"github.com/danushkastanley/kube-memlens/internal/trace"
	"time"
)

// Expiry permits only a bounded final-result drain. It does not authorise more
// event callbacks, even if a worker sends a formerly observed record late.
type activeOutput struct {
	ctx      context.Context
	deadline time.Time
	target   trace.Output
}

func (o activeOutput) FileActivity(event trace.FileActivity) error {
	if !o.active() {
		return ErrWorker
	}
	return o.target.FileActivity(event)
}
func (o activeOutput) CacheActivity(event trace.CacheActivity) error {
	if !o.active() {
		return ErrWorker
	}
	return o.target.CacheActivity(event)
}
func (o activeOutput) OOMDecision(event trace.OOMDecision) error {
	if !o.active() {
		return ErrWorker
	}
	return o.target.OOMDecision(event)
}

func (o activeOutput) active() bool { return o.ctx.Err() == nil && time.Now().Before(o.deadline) }
