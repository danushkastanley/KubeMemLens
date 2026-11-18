//go:build !linux

package main

import (
	"fmt"
	"os"
)

// The observer reads Linux clocks, cgroups and /proc. This entry point exists
// so that `go build ./...` links on a development host that is not Linux.
func main() {
	fmt.Fprintln(os.Stderr, "the metrics observer runs on Linux only")
	os.Exit(2)
}
