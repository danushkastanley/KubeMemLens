package sdk

import (
	"context"
	"errors"
	"testing"
	"time"
)

type flushFunc func() error

func (f flushFunc) Flush() error { return f() }

func TestCancellationInterruptsIdleReader(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	flushed := make(chan struct{})
	finish := interruptRead(ctx, flushFunc(func() error { close(flushed); return nil }))
	cancel()
	select {
	case <-flushed:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not wake the reader")
	}
	if err := finish(); err != nil {
		t.Fatal(err)
	}
}

func TestFinishedReaderDoesNotReceiveLateFlush(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	flushed := make(chan struct{}, 1)
	finish := interruptRead(ctx, flushFunc(func() error { flushed <- struct{}{}; return nil }))
	if err := finish(); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-flushed:
		t.Fatal("finished reader received a late callback")
	default:
	}
}

func TestReaderFinalisationJoinsFailedCancellationWake(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	failure := errors.New("fixture wake failure")
	finish := interruptRead(ctx, flushFunc(func() error { close(entered); <-release; return failure }))
	cancel()
	<-entered
	result := make(chan error, 1)
	go func() { result <- finish() }()
	select {
	case <-result:
		t.Fatal("reader finalised while callback still owned it")
	default:
	}
	close(release)
	select {
	case err := <-result:
		if !errors.Is(err, failure) {
			t.Fatal("wake failure hidden")
		}
	case <-time.After(time.Second):
		t.Fatal("reader finalisation did not join callback")
	}
}
