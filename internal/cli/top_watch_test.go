package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/client"
)

var watchTestOptions = topOptions{Watch: true, WatchInterval: 5 * time.Millisecond}

func unavailableRefresh() error {
	return fmt.Errorf("Could not connect to the collector.\n\nTry:\n  a long hint\n\nUnderlying error: %w",
		&client.ReadError{Kind: client.ReadErrorUnavailable, Operation: "list Pods", Cause: context.DeadlineExceeded})
}

// scriptedRender plays one step per call. The refresh after the last step
// cancels the watch, so every scripted frame has been drawn by then.
func scriptedRender(cancel context.CancelFunc, steps ...func(io.Writer) error) (func(io.Writer) error, *int) {
	calls := 0
	return func(w io.Writer) error {
		if calls == len(steps) {
			cancel()
			return nil
		}
		step := steps[calls]
		calls++
		return step(w)
	}, &calls
}

func frame(text string) func(io.Writer) error {
	return func(w io.Writer) error { _, err := fmt.Fprintln(w, text); return err }
}

func TestWatchTopKeepsWatchingThroughAnUnavailableRefresh(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var output bytes.Buffer
	render, calls := scriptedRender(cancel,
		frame("frame one"),
		func(w io.Writer) error { fmt.Fprintln(w, "half a frame"); return unavailableRefresh() },
		frame("frame three"))
	if err := watchTop(ctx, &output, watchTestOptions, render); err != nil {
		t.Fatalf("watchTop = %v, want the watch to continue and end cleanly", err)
	}
	text := output.String()
	if *calls != 3 || !strings.Contains(text, "frame one") || !strings.Contains(text, "frame three") {
		t.Fatalf("calls=%d output:\n%s", *calls, text)
	}
	if !strings.Contains(text, "Refresh unavailable; last successful read") || !strings.Contains(text, "Could not connect to the collector.") {
		t.Fatalf("the failed refresh is not reported:\n%s", text)
	}
	for _, unwanted := range []string{"half a frame", "a long hint"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("output contains %q:\n%s", unwanted, text)
		}
	}
}

func TestWatchTopStopsOnAnErrorThatWillNotClear(t *testing.T) {
	permanent := map[string]error{
		"forbidden":     fmt.Errorf("no permission: %w", &client.ReadError{Kind: client.ReadErrorForbidden, Operation: "list Pods"}),
		"not installed": errors.New("the KubeMemLens API is not installed"),
	}
	for name, refreshErr := range permanent {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var output bytes.Buffer
			render, calls := scriptedRender(func() {},
				frame("frame one"),
				func(io.Writer) error { return refreshErr },
				frame("never drawn"))
			err := watchTop(ctx, &output, watchTestOptions, render)
			if !errors.Is(err, refreshErr) || *calls != 2 {
				t.Fatalf("watchTop = %v after %d calls, want the refresh error after 2", err, *calls)
			}
		})
	}
}

func TestWatchTopFailsWhenTheFirstReadFails(t *testing.T) {
	render, calls := scriptedRender(func() {}, func(io.Writer) error { return unavailableRefresh() }, frame("never drawn"))
	err := watchTop(context.Background(), &bytes.Buffer{}, watchTestOptions, render)
	if !client.IsUnavailable(err) || *calls != 1 {
		t.Fatalf("watchTop = %v after %d calls, want the first error after 1", err, *calls)
	}
}

func TestWatchTopWritesNoTerminalEscapesToAPipe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var output bytes.Buffer
	render, _ := scriptedRender(cancel, frame("frame one"), frame("frame two"), frame("frame three"))
	if err := watchTop(ctx, &output, watchTestOptions, render); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "\x1b") {
		t.Fatalf("output written to a pipe contains terminal escapes: %q", output.String())
	}
	if got := strings.Count(output.String(), "frame"); got != 3 {
		t.Fatalf("frames written = %d, want 3:\n%s", got, output.String())
	}
}
