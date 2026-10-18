package workerprocess

import (
	"context"
	"github.com/danushkastanley/kube-memlens/internal/trace"
	"testing"
	"time"
)

type batchOutputFixture struct {
	outputFixture
	calls int
}

func (o *batchOutputFixture) FileActivities(events []trace.FileActivity) error {
	o.calls++
	o.events += len(events)
	return nil
}
func TestFileBatchCannotBypassActiveWindow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	target := &batchOutputFixture{}
	out := activeOutput{ctx: ctx, deadline: time.Now().Add(time.Minute), target: target}
	events := make([]trace.FileActivity, 2)
	if err := out.FileActivities(events); err != nil || target.calls != 1 || target.events != 2 {
		t.Fatal("active batch not forwarded once")
	}
	out.deadline = time.Now().Add(-time.Second)
	if err := out.FileActivities(events); err != ErrWorker || target.calls != 1 {
		t.Fatal("batch forwarded during expiry drain")
	}
	out.deadline = time.Now().Add(time.Minute)
	cancel()
	if err := out.FileActivities(events); err != ErrWorker || target.calls != 1 {
		t.Fatal("batch forwarded after cancellation")
	}
}
