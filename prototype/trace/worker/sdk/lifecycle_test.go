package sdk

import (
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/inspektor-gadget/inspektor-gadget/pkg/operators"
)

type cleanupFixture struct {
	calls *[]string
	fail  string
}

func (f cleanupFixture) action(name string) error {
	*f.calls = append(*f.calls, name)
	if f.fail == name {
		return ErrWorker
	}
	return nil
}
func (f cleanupFixture) disable() error                      { return f.action("disable") }
func (f cleanupFixture) closeReader() error                  { return f.action("reader") }
func (f cleanupFixture) Name() string                        { return "fixture" }
func (f cleanupFixture) Start(operators.GadgetContext) error { return nil }
func (f cleanupFixture) Stop(operators.GadgetContext) error  { return f.action("stop") }
func (f cleanupFixture) Close(operators.GadgetContext) error { return f.action("close") }

func TestCleanupAttemptsAllStepsAndRetainsFailures(t *testing.T) {
	for _, failed := range []string{"", "disable", "reader", "stop", "close"} {
		t.Run("failure-"+failed, func(t *testing.T) {
			var calls []string
			fixture := cleanupFixture{&calls, failed}
			l := &lifecycle{instance: fixture, resources: fixture}
			_ = l.stop()
			for range 2 {
				err := l.close()
				if (err != nil) != (failed != "") || (err != nil && !errors.Is(err, ErrWorker)) {
					t.Fatal("cleanup failure lost")
				}
			}
			if !reflect.DeepEqual(calls, []string{"disable", "reader", "stop", "close"}) {
				t.Fatal("cleanup repeated or skipped an operation")
			}
		})
	}
}

func TestConcurrentFinalisationRunsCleanupOnce(t *testing.T) {
	var calls []string
	f := cleanupFixture{calls: &calls}
	l := &lifecycle{instance: f, resources: f}
	var group sync.WaitGroup
	for range 10 {
		group.Go(func() { _ = l.close() })
	}
	group.Wait()
	if !reflect.DeepEqual(calls, []string{"disable", "reader", "stop", "close"}) {
		t.Fatal("concurrent finalisation repeated cleanup")
	}
}
