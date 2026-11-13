package cli

import (
	"bytes"
	"strings"
	"testing"
)

// A capture comes from someone else and is replayed on this terminal. A crafted
// name must be refused before anything from the file is printed.
func TestReplayAndCompareRefuseTerminalControlsInACapture(t *testing.T) {
	crafted := oomKilledBeforeCapture("checkout", capturedLongAgo)
	crafted.PodName = "checkout\x1b[2J\x1b[31mPWNED"
	crafted.Containers[0].PodName = crafted.PodName
	hostile := writeDeepBundle(t, "hostile.json", capturedLongAgo, crafted)
	clean := writeDeepBundle(t, "clean.json", capturedLongAgo, oomKilledBeforeCapture("checkout", capturedLongAgo))

	for name, run := range map[string]func(*bytes.Buffer) error{
		"replay": func(output *bytes.Buffer) error {
			command := newReplayCommand()
			command.SetOut(output)
			command.SetErr(output)
			command.SetArgs([]string{hostile})
			return command.Execute()
		},
		"compare": func(output *bytes.Buffer) error {
			command := newCompareCommand(nil)
			command.SetOut(output)
			command.SetErr(output)
			command.SetArgs([]string{"--before", clean, "--after", hostile, "--pod", "shop/checkout"})
			return command.Execute()
		},
	} {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			err := run(&output)
			if err == nil || !strings.Contains(err.Error(), "control characters") {
				t.Fatalf("error = %v, want the capture to be refused", err)
			}
			if strings.Contains(output.String(), "\x1b") || strings.Contains(err.Error(), "\x1b") || strings.Contains(output.String(), "PWNED") {
				t.Fatalf("text from the crafted capture reached the terminal: %q", output.String())
			}
		})
	}
}
