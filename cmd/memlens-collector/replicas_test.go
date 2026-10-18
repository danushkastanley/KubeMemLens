package main

import (
	"flag"
	"testing"
)

func TestReplicaFlagsRequireAuthenticatedExplicitScope(t *testing.T) {
	for _, tc := range []struct {
		name, mode     string
		args           []string
		valid, enabled bool
	}{
		{"default", ingestionAuthenticated, nil, true, false},
		{"enabled", ingestionAuthenticated, []string{"--replica-baselines", "--replica-namespaces=team-a,team-b"}, true, true},
		{"unscoped", ingestionAuthenticated, []string{"--replica-baselines"}, false, false},
		{"unused", ingestionAuthenticated, []string{"--replica-namespaces=team-a"}, false, false},
		{"duplicate", ingestionAuthenticated, []string{"--replica-baselines", "--replica-namespaces=team-a,team-a"}, false, false},
		{"wildcard", ingestionAuthenticated, []string{"--replica-baselines", "--replica-namespaces=*"}, false, false},
		{"legacy", ingestionLegacy, []string{"--replica-baselines", "--replica-namespaces=team-a"}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flags := flag.NewFlagSet("replicas", flag.ContinueOnError)
			f := registerReplicaFlags(flags)
			if err := flags.Parse(tc.args); err != nil {
				t.Fatal(err)
			}
			result, err := f.resolve(tc.mode)
			if (err == nil) != tc.valid || (result != nil) != tc.enabled {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}
