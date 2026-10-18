package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestAllocationBounds(t *testing.T) {
	for _, args := range [][]string{{"--hold-mib=65"}, {"--hold-mib=-1"}, {"--duration=16m"}, {"--duration=0"}, {"unexpected"}} {
		if _, err := parse(args); err == nil {
			t.Fatal("unbounded fixture accepted", args)
		}
	}
	if result, err := parse([]string{"--hold-mib=64", "--duration=12m"}); err != nil || result.mib != 64 {
		t.Fatal(err)
	}
}
func TestAllocationCancellationAndReceipt(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var receipt bytes.Buffer
	run(ctx, options{mib: 1, duration: time.Minute}, &receipt)
	if !strings.Contains(receipt.String(), "holding 1048576 bytes") {
		t.Fatal("allocation receipt missing")
	}
	run(ctx, options{}, &receipt)
}
