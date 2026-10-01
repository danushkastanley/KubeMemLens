package main

import (
	"slices"
	"testing"
)

func TestCompleteCPURosterHasFixedBounds(t *testing.T) {
	got, err := onlineCPUs([]byte("0-2,4-5\n"))
	if err != nil || !slices.Equal(got, []uint32{0, 1, 2, 4, 5}) {
		t.Fatal("valid CPU topology rejected")
	}
	for _, raw := range []string{"", "0", "0-64", "1,0", "0,0", "0-0", "0-1,1-2", "0-1,1024", "0-1,", "-1,0"} {
		if _, err := onlineCPUs([]byte(raw)); err == nil {
			t.Fatal("partial, duplicate or unbounded topology accepted")
		}
	}
}
