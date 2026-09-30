package main

import (
	"flag"
	"testing"
)

func TestIncidentSessionFlagsRequireExplicitAuthenticatedScope(t *testing.T) {
	for _, test := range []struct {
		name, mode     string
		args           []string
		valid, enabled bool
	}{
		{"default", ingestionAuthenticated, nil, true, false},
		{"enabled", ingestionAuthenticated, []string{"--incident-sessions", "--incident-session-namespaces=team-a,team-b"}, true, true},
		{"unscoped", ingestionAuthenticated, []string{"--incident-sessions"}, false, false},
		{"unused", ingestionAuthenticated, []string{"--incident-session-namespaces=team-a"}, false, false},
		{"duplicate", ingestionAuthenticated, []string{"--incident-sessions", "--incident-session-namespaces=team-a,team-a"}, false, false},
		{"wildcard", ingestionAuthenticated, []string{"--incident-sessions", "--incident-session-namespaces=*"}, false, false},
		{"legacy", ingestionLegacy, []string{"--incident-sessions", "--incident-session-namespaces=team-a"}, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			flags := flag.NewFlagSet("incident-sessions", flag.ContinueOnError)
			f := registerIncidentSessionFlags(flags)
			if err := flags.Parse(test.args); err != nil {
				t.Fatal(err)
			}
			result, err := f.resolve(test.mode)
			if (err == nil) != test.valid || (result != nil) != test.enabled {
				t.Fatalf("valid=%v enabled=%v err=%v", test.valid, test.enabled, err)
			}
		})
	}
}
