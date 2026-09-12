package kube

import (
	"context"

	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

func (r *volumeResolver) bindingQuery() volumeBindingQuery {
	q := volumeBindingQuery{healthQuery: healthQuery{reader: r.reader, remaining: maxHealthQueryBytes, validateJSON: boundedVolumeObject}}
	q.beforeRequest = r.calls.Wait
	q.authorize = func(ctx context.Context, access VolumeAccess) error {
		if err := r.calls.Wait(ctx); err != nil {
			return &HealthReadError{Reason: volumehealth.ReadFailed, cause: err}
		}
		return r.authorize(ctx, access)
	}
	return q
}
