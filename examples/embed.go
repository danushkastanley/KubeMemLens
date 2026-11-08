// Package examples embeds the bundled cgroup v2 samples so the released CLI
// can explain them without a source checkout.
package examples

import (
	"embed"
	"io/fs"
)

//go:embed cgroup-v2
var cgroupV2 embed.FS

// CgroupV2 returns the sample tree rooted at examples/cgroup-v2, with one
// directory per sample.
func CgroupV2() (fs.FS, error) {
	return fs.Sub(cgroupV2, "cgroup-v2")
}
