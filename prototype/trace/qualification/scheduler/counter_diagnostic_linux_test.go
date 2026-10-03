package scheduler

import (
	"errors"
	"strings"
	"testing"
)

func TestCoverageFollowUpNeverAcceptsRejectedObservation(t *testing.T) {
	for _, test := range []struct {
		name, expected string
		raw            []byte
		err            error
	}{
		{"recovered", "follow_enabled=200 follow_running=200 follow_lost=0", countRecord(200, 200, 0), nil},
		{"persistent", "follow_enabled=200 follow_running=199 follow_lost=0", countRecord(200, 199, 0), nil},
		{"lost", "follow_enabled=200 follow_running=200 follow_lost=3", countRecord(200, 200, 3), nil},
		{"short", "follow_read_bytes=8 follow_read_failed=false", make([]byte, 8), nil},
		{"error", "follow_read_bytes=0 follow_read_failed=true", nil, errors.New("untrusted read detail")},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			read := func(fd int, raw []byte) (int, error) {
				calls++
				if fd != 98765 || calls > 2 {
					t.Fatal("read another descriptor or retried the follow-up")
				}
				if calls == 1 {
					return copy(raw, countRecord(100, 99, 0)), nil
				}
				return copy(raw, test.raw), test.err
			}
			counts, err := readCounters([]int{98765, 45678}, read)
			if calls != 2 || counts != (Counters{}) || !errors.Is(err, ErrCoverage) {
				t.Fatal("follow-up changed the failed coverage result", calls, counts, err)
			}
			message := err.Error()
			if !strings.HasPrefix(message, "descriptor=0: ") || !strings.Contains(message, "enabled=100 running=99") ||
				!strings.Contains(message, test.expected) || strings.Contains(message, "98765") ||
				strings.Contains(message, "untrusted read detail") {
				t.Fatal("numeric diagnostic changed its cause or exposed other detail", message)
			}
		})
	}
}

func TestCounterFollowUpOnlyRunsAfterCoverageMismatch(t *testing.T) {
	for _, test := range []struct {
		name string
		raw  []byte
		err  error
	}{
		{"valid", countRecord(100, 100, 0), nil},
		{"loss", countRecord(100, 100, 1), nil},
		{"short", make([]byte, 8), nil},
		{"error", nil, ErrObservation},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			readCounters([]int{98765}, func(_ int, raw []byte) (int, error) {
				calls++
				return copy(raw, test.raw), test.err
			})
			if calls != 1 {
				t.Fatal("unnecessary diagnostic read", calls)
			}
		})
	}
}
