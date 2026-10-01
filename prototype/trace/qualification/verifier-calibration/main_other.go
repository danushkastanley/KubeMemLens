//go:build !linux

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "fixed verifier calibration requires an owned Linux node")
	os.Exit(2)
}
