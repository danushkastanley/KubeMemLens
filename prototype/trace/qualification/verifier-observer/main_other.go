//go:build !linux

package main

import (
	"fmt"
	"os"
)

func main() { fmt.Fprintln(os.Stderr, "verifier observation requires an owned Linux node"); os.Exit(2) }
