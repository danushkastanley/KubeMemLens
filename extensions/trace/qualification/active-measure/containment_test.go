package main

import (
	"encoding/json"
	"testing"
)

func TestCPULimitDistinguishesUnlimitedFromNumericQuota(t *testing.T) {
	for _, input := range []string{"200000 100000\n", "max 100000\n"} {
		value, err := parseCPULimit([]byte(input))
		if err != nil || value.Period != 100000 {
			t.Fatal(value, err)
		}
		if input[0] == 'm' {
			if value.Maximum.State != "unlimited" || value.Maximum.Value != nil {
				t.Fatal(value)
			}
		} else if value.Maximum.State != "observed" || *value.Maximum.Value != 200000 {
			t.Fatal(value)
		}
	}
	for _, input := range []string{"", "max", "0 100000", "1 0", "-1 100000", "+1 100000", "1 max", "1 2 3", "18446744073709551616 100000"} {
		if _, err := parseCPULimit([]byte(input)); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
}

func TestNumericZeroAndUnavailableHaveDifferentRepresentations(t *testing.T) {
	zero, err := numericObservation("0")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(zero)
	if string(encoded) != `{"state":"observed","value":0}` {
		t.Fatal(string(encoded))
	}
	encoded, _ = json.Marshal(observedLimit{State: "unavailable"})
	if string(encoded) != `{"state":"unavailable"}` {
		t.Fatal(string(encoded))
	}
	for _, input := range []string{"max", "", "1 2", "1\n", "NaN", "1.0", "-1", "+1"} {
		if _, err := numericObservation(input); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
}
