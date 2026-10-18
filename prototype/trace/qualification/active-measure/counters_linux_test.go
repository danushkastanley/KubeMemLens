//go:build linux

package main

import (
	"strings"
	"testing"
)

func TestCountersRejectMissingMalformedAndDuplicates(t *testing.T) {
	for _, text := range []string{"", "usage_usec", "usage_usec -1", "usage_usec 1 usage_usec 2", "usage_usec NaN"} {
		if _, err := counters([]byte(text)); err == nil {
			t.Fatalf("accepted %q", text)
		}
	}
	c, err := counters([]byte("usage_usec 123\nthrottled_usec 4\n"))
	if err != nil || c["usage_usec"] != 123 || c["throttled_usec"] != 4 {
		t.Fatal(c, err)
	}
}

func TestBoundedCounterRead(t *testing.T) {
	if _, err := readBounded(strings.NewReader(strings.Repeat("x", 65537))); err == nil {
		t.Fatal("unbounded read")
	}
}

func TestPressureRequiresActualTotals(t *testing.T) {
	for _, s := range []string{"", "some avg10=0 avg60=0 avg300=0", "some avg10=0 avg60=0 avg300=0 total=-1"} {
		if _, err := pressure([]byte(s)); err == nil {
			t.Fatal("accepted missing/negative observation")
		}
	}
	v, err := pressure([]byte("some avg10=0.00 avg60=0.00 avg300=0.00 total=42\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=1\n"))
	if err != nil || v["some"] != 42 || v["full"] != 1 {
		t.Fatal(v, err)
	}
}

func TestProcessSetBoundsAndDuplicates(t *testing.T) {
	ids, err := processIDs([]byte("3\n2\n3\n"))
	if err != nil || len(ids) != 2 || ids[0] != 2 || ids[1] != 3 {
		t.Fatal(ids, err)
	}
	for _, s := range []string{"", "1", "-2", strings.Repeat("2\n", 65)} {
		if _, err := processIDs([]byte(s)); err == nil {
			t.Fatal("invalid process set accepted")
		}
	}
}

func TestGroupRejectsUnscopedOrUnboundPaths(t *testing.T) {
	for _, spec := range []groupSpec{{"node", "/sys/fs/cgroup", 1}, {"api", "/sys/fs/cgroup/x/../y", 1}, {"api", "/sys/fs/cgroup/x", 0}, {"other", "/sys/fs/cgroup/x", 1}} {
		if _, err := openGroup(spec); err == nil {
			t.Fatal("invalid binding accepted")
		}
	}
}
