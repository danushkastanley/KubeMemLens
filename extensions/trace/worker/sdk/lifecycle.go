package sdk

import (
	"errors"
	"sync"

	"github.com/inspektor-gadget/inspektor-gadget/pkg/operators"
)

type sessionResources interface {
	disable() error
	closeReader() error
}

// lifecycle attempts every cleanup operation and preserves the first result.
// A repeated finalisation cannot close a descriptor reused by a later owner.
type lifecycle struct {
	context   operators.GadgetContext
	instance  operators.ImageOperatorInstance
	resources sessionResources
	stopOnce  sync.Once
	closeOnce sync.Once
	stopErr   error
	closeErr  error
}

func (l *lifecycle) stop() error {
	l.stopOnce.Do(func() {
		l.stopErr = errors.Join(l.resources.disable(), l.resources.closeReader(), l.instance.Stop(l.context))
	})
	return l.stopErr
}

func (l *lifecycle) close() error {
	l.closeOnce.Do(func() {
		l.closeErr = errors.Join(l.stop(), l.instance.Close(l.context))
	})
	return l.closeErr
}
