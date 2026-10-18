package main

import (
	"flag"
	"testing"
)

func TestRemoteHistoryFlagsAreExplicitAndAuthenticated(t *testing.T) {
	for _, tc := range []struct {
		name, mode     string
		args           []string
		valid, enabled bool
	}{
		{"default", ingestionLegacy, nil, true, false},
		{"markers without history", ingestionAuthenticated, []string{"--memory-change-markers"}, false, false},
		{"node markers", ingestionAuthenticated, []string{"--remote-history-enabled", "--remote-history-url=https://history.example", "--remote-history-cluster=test", "--remote-history-ca-file=/trust/ca.crt", "--remote-history-nodes", "--memory-change-markers"}, false, false},
		{"namespace markers", ingestionAuthenticated, []string{"--remote-history-enabled", "--remote-history-url=https://history.example", "--remote-history-cluster=test", "--remote-history-ca-file=/trust/ca.crt", "--remote-history-namespaces=team-a", "--memory-change-markers"}, true, true},
		{"unused configuration", ingestionAuthenticated, []string{"--remote-history-url=https://history.example"}, false, false},
		{"legacy", ingestionLegacy, []string{"--remote-history-enabled", "--remote-history-url=https://history.example", "--remote-history-cluster=test", "--remote-history-ca-file=/trust/ca.crt", "--remote-history-namespaces=team-a"}, false, false},
		{"missing scope", ingestionAuthenticated, []string{"--remote-history-enabled", "--remote-history-url=https://history.example", "--remote-history-cluster=test"}, false, false},
		{"namespace", ingestionAuthenticated, []string{"--remote-history-enabled", "--remote-history-url=https://history.example", "--remote-history-cluster=test", "--remote-history-ca-file=/trust/ca.crt", "--remote-history-namespaces=team-a"}, true, true},
		{"node only", ingestionAuthenticated, []string{"--remote-history-enabled", "--remote-history-url=https://history.example", "--remote-history-cluster=test", "--remote-history-ca-file=/trust/ca.crt", "--remote-history-nodes"}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flags := flag.NewFlagSet("test", flag.ContinueOnError)
			f := registerMemoryHistoryFlags(flags)
			if err := flags.Parse(tc.args); err != nil {
				t.Fatal(err)
			}
			o, err := f.resolve(tc.mode)
			if (err == nil) != tc.valid || (o != nil) != tc.enabled {
				t.Fatalf("configuration: %v %+v", err, o)
			}
		})
	}
}
