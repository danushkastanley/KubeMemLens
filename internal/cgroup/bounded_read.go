package cgroup

import (
	"fmt"
	"io"
	"io/fs"
)

// maxCgroupFileBytes bounds one cgroup interface file. memory.stat is the
// largest the agent reads and is a few kilobytes.
const maxCgroupFileBytes = 256 << 10

// readBounded reads a cgroup interface file of at most maxCgroupFileBytes. A
// larger file is an error, not a truncated read: parsing a prefix would report
// part of the evidence as the whole.
func readBounded(fsys fs.FS, name string) ([]byte, error) {
	file, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxCgroupFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxCgroupFileBytes {
		return nil, fmt.Errorf("%s is larger than %d bytes", name, maxCgroupFileBytes)
	}
	return data, nil
}
