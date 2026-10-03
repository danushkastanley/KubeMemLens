package sdk

import (
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func TestFileReadDoesNotWaitForNextEventOrTargetValidation(t *testing.T) {
	now := time.Unix(100, 0)
	validation := now.Add(time.Second)
	if got := ringReadDeadline(trace.Files, now, validation); !got.Equal(now.Add(10 * time.Millisecond)) {
		t.Fatal("sparse file record could wait for another event or target validation")
	}
	// Repeated empty-ring polls do not move the independent validation time.
	for i := 1; i < 100; i++ {
		at := now.Add(time.Duration(i) * 10 * time.Millisecond)
		got := ringReadDeadline(trace.Files, at, validation)
		if got.After(at.Add(10*time.Millisecond)) || got.After(validation) || !got.After(at) {
			t.Fatal("file polling exceeded delivery or validation bound")
		}
	}
}

func TestFileReadHonoursEarlierAndDueTargetValidation(t *testing.T) {
	now := time.Unix(100, 0)
	for _, delta := range []time.Duration{-time.Millisecond, 0, time.Millisecond, 10 * time.Millisecond} {
		validation := now.Add(delta)
		if got := ringReadDeadline(trace.Files, now, validation); !got.Equal(validation) {
			t.Fatal("file polling postponed target validation")
		}
	}
}

func TestNotifiedProfilesKeepTargetValidationDeadline(t *testing.T) {
	now := time.Unix(100, 0)
	validation := now.Add(time.Second)
	for _, kind := range []trace.Kind{trace.Cache, trace.OOM} {
		if got := ringReadDeadline(kind, now, validation); !got.Equal(validation) {
			t.Fatal("notified profile gained unnecessary polling")
		}
	}
}
