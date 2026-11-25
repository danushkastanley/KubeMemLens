package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/danushkastanley/kube-memlens/internal/cli"
)

func main() {
	err := cli.Execute(os.Stdout, os.Stderr)
	if errors.Is(err, cli.ErrInterrupted) {
		// 128 plus SIGINT, as a shell reports a command ended by Ctrl+C.
		os.Exit(130)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
