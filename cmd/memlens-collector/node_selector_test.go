package main

import "testing"

func TestParseExpectedNodeSelector(t *testing.T) {
	selector, err := parseExpectedNodeSelector(`{"topology.kubernetes.io/zone":"zone-a","kubernetes.io/os":"linux"}`)
	if err != nil {
		t.Fatal(err)
	}
	if selector != "kubernetes.io/os=linux,topology.kubernetes.io/zone=zone-a" {
		t.Fatalf("selector = %q", selector)
	}
	for _, invalid := range []string{"", `{"bad key":"value"}`, `{} trailing`} {
		if _, err := parseExpectedNodeSelector(invalid); err == nil {
			t.Fatalf("invalid selector %q was accepted", invalid)
		}
	}
}

func TestParseExpectedNodeTolerations(t *testing.T) {
	values, err := parseExpectedNodeTolerations(`[{"key":"dedicated","operator":"Equal","value":"memory","effect":"NoSchedule"}]`)
	if err != nil || len(values) != 1 || values[0].Key != "dedicated" {
		t.Fatalf("tolerations=%#v error=%v", values, err)
	}
	for _, invalid := range []string{"", `[{"key":"bad key","operator":"Exists"}]`, `[{"key":"dedicated","operator":"Exists","value":"bad"}]`} {
		if _, err := parseExpectedNodeTolerations(invalid); err == nil {
			t.Fatalf("invalid tolerations %q were accepted", invalid)
		}
	}
}
