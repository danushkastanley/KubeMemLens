package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/cgroup"
	"github.com/danushkastanley/kube-memlens/internal/client"
)

// nodeCgroupVersions counts reporting nodes by the cgroup version their agent
// reported. KubeMemLens reads cgroup v2 only. cgroup v1, any other version and
// a version that was not reported are kept apart because each needs a
// different response, and none of them means the node runs no containers.
type nodeCgroupVersions struct {
	reporting  int
	v1         int
	other      int
	unreported int
}

func countNodeCgroupVersions(nodes []api.NodeSnapshotStatus) nodeCgroupVersions {
	var counts nodeCgroupVersions
	for _, node := range nodes {
		if node.Freshness == api.EvidenceFreshnessMissing {
			continue
		}
		counts.reporting++
		switch node.Environment.CgroupVersion {
		case cgroup.VersionV2:
		case cgroup.VersionV1:
			counts.v1++
		case cgroup.VersionUnknown:
			counts.unreported++
		default:
			counts.other++
		}
	}
	return counts
}

// check returns the doctor status and summary for the counts. Doctor and
// status show the same summary. Only the cgroup v1 wording is new; the other
// two keep doctor's earlier text.
func (c nodeCgroupVersions) check() (string, string) {
	var parts []string
	if c.v1 > 0 {
		parts = append(parts, fmt.Sprintf(
			"%d of %d reporting nodes use cgroup v1; KubeMemLens reads cgroup v2 only, so those nodes have no container memory evidence (see docs/compatibility.md)",
			c.v1, c.reporting))
	}
	if c.other > 0 {
		parts = append(parts, fmt.Sprintf("%d reporting nodes are not using supported cgroup v2", c.other))
	}
	if c.unreported > 0 {
		parts = append(parts, fmt.Sprintf("%d reporting nodes did not report their cgroup version", c.unreported))
	}
	switch {
	case c.v1+c.other > 0:
		return "fail", strings.Join(parts, "; ")
	case c.unreported > 0:
		return "warn", strings.Join(parts, "; ")
	default:
		return "pass", "all reporting nodes use cgroup v2"
	}
}

// statusCgroup is the cgroup version fact status adds to its Reliability
// block. The node read behind it is supplementary: a caller who may read the
// cluster status but not the node list still gets the full report.
type statusCgroup struct {
	versions   nodeCgroupVersions
	notChecked bool
}

func readStatusCgroup(ctx context.Context, reader client.SnapshotReader) statusCgroup {
	nodes, err := reader.Nodes(ctx)
	if err != nil {
		return statusCgroup{notChecked: true}
	}
	return statusCgroup{versions: countNodeCgroupVersions(nodes)}
}

// line returns nothing when every reporting node uses cgroup v2. The raw read
// error is left out: it can carry the collector URL.
func (s statusCgroup) line() string {
	if s.notChecked {
		return "  cgroup version: not checked; node status could not be read\n"
	}
	status, summary := s.versions.check()
	if status == "pass" {
		return ""
	}
	return "  cgroup version: " + summary + "\n"
}
