package main

import (
	"fmt"
	"os"

	"github.com/danushkastanley/kube-memlens/internal/cli"
)

func main() {
	if err := cli.Execute(os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
