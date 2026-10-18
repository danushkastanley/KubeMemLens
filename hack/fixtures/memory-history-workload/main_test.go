package main

import (
	"context"
	"testing"
	"time"
)

func TestFixtureLifetimeBounds(t *testing.T) {
	for _, test := range []struct {
		args  []string
		valid bool
		delay time.Duration
	}{
		{nil, true, 0}, {[]string{"--exit-after=30s"}, true, 30 * time.Second},
		{[]string{"--exit-after=2m"}, true, 2 * time.Minute},
		{[]string{"--exit-after=-1s"}, false, 0}, {[]string{"--exit-after=121s"}, false, 0},
		{[]string{"--exit-after=1s", "extra"}, false, 0}, {[]string{"--unknown"}, false, 0},
	} {
		delay, err := exitDelay(test.args)
		if (err == nil) != test.valid || (err == nil && delay != test.delay) {
			t.Fatal(test.args, delay, err)
		}
	}
}

func TestFixtureStopsOnCancellationOrBoundedLifetime(t *testing.T) {
	for _, delay := range []time.Duration{0, time.Millisecond} {
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() { wait(ctx, delay); close(done) }()
		if delay == 0 {
			cancel()
		}
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("fixture did not stop")
		}
		cancel()
	}
}
