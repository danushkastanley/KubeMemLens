package main

import (
	"errors"
	"testing"
	"time"
)

func TestIndependentBindingsOverlapAndBothFinishBeforeReturn(t *testing.T) {
	entered := make(chan string, 2)
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	finished := make(chan error, 1)
	check := func(name string) func() error {
		return func() error {
			entered <- name
			<-release
			return nil
		}
	}
	go func() { finished <- verifyBindings(check("agent"), check("collector")) }()
	seen := map[string]bool{}
	for range 2 {
		select {
		case name := <-entered:
			seen[name] = true
		case <-time.After(time.Second):
			t.Fatal("independent verifications did not overlap")
		}
	}
	if len(seen) != 2 {
		t.Fatal("same binding verified twice")
	}
	select {
	case <-finished:
		t.Fatal("returned before both bindings completed")
	default:
	}
	close(release)
	released = true
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("verification did not finish after both callbacks were released")
	}
}

func TestBindingErrorsStayDeterministicAndPrivate(t *testing.T) {
	private := errors.New("private process identity")
	for _, test := range []struct {
		name, stage      string
		agent, collector error
	}{
		{"success", "", nil, nil},
		{"agent", "agent-binding", private, nil},
		{"collector", "collector-binding", nil, private},
		{"both", "agent-binding", private, private},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := make(chan string, 2)
			err := verifyBindings(func() error { calls <- "agent"; return test.agent },
				func() error { calls <- "collector"; return test.collector })
			if len(calls) != 2 {
				t.Fatal("did not await both verifications")
			}
			if test.stage == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || failureDetail(err) != test.stage {
				t.Fatal("changed failure priority or exposed private error", err)
			}
		})
	}
}
