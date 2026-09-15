// inspect-filecache produces a static review inventory without kernel loading.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
)

func main() {
	if run() != nil {
		fmt.Fprintln(os.Stderr, "file/cache object inspection failed")
		os.Exit(1)
	}
}
func run() error {
	kind := flag.String("kind", "", "fixed trace kind: files or cache")
	path := flag.String("object", "", "local candidate ELF path")
	flag.Parse()
	if flag.NArg() != 0 {
		return filecache.ErrObject
	}
	f, err := os.Open(*path)
	if err != nil {
		return filecache.ErrObject
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, filecache.MaxObjectBytes+1))
	if err != nil {
		return filecache.ErrObject
	}
	inventory, err := filecache.Inspect(trace.Kind(*kind), data)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(inventory)
}
