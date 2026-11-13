package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/observationview"
)

const clearScreen = "\x1b[H\x1b[2J"

// watchTop draws one frame, then redraws it on every interval while watching.
// Each frame is rendered in full before anything is written, so a failed
// refresh leaves the last good frame on screen. A refresh that fails because
// the collector is unavailable is reported and retried; any other failure will
// not clear by waiting, so it ends the watch.
func watchTop(ctx context.Context, w io.Writer, opts topOptions, render func(io.Writer) error) error {
	var frame bytes.Buffer
	if err := render(&frame); err != nil {
		return err
	}
	if _, err := frame.WriteTo(w); err != nil {
		return err
	}
	if !opts.Watch {
		return nil
	}
	clear := ""
	if isTerminal(w) {
		clear = clearScreen
	}
	lastRead := time.Now()
	ticker := time.NewTicker(opts.WatchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			frame.Reset()
			err := render(&frame)
			if ctx.Err() != nil {
				return nil
			}
			if err != nil && !client.IsUnavailable(err) {
				// Do not leave memory data on screen once a read is refused.
				fmt.Fprint(w, clear)
				return err
			}
			if err != nil {
				summary, _, _ := strings.Cut(err.Error(), "\n")
				if _, writeErr := fmt.Fprintf(w, "\nRefresh unavailable; last successful read %s ago: %s\n", observationview.Age(lastRead, time.Now()), summary); writeErr != nil {
					return writeErr
				}
				continue
			}
			if _, writeErr := io.WriteString(w, clear); writeErr != nil {
				return writeErr
			}
			if _, writeErr := frame.WriteTo(w); writeErr != nil {
				return writeErr
			}
			lastRead = time.Now()
		}
	}
}

// isTerminal reports whether w is a character device, which is where a
// clear-screen escape belongs. Pipes and files get plain frames.
func isTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
