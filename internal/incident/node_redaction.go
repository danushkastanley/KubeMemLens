package incident

import (
	"crypto/sha256"
	"fmt"
	"regexp"

	"github.com/danushkastanley/kube-memlens/internal/nodeanalysis"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

var nodeFingerprintPattern = regexp.MustCompile(`^sha256-[a-f0-9]{64}$`)
var nodeAliasPattern = regexp.MustCompile(`^(namespace|pod|workload)-[1-9][0-9]*$`)

// Fingerprints permit same-instance comparison without retaining raw Node UIDs.
// They remain correlatable pseudonyms, not anonymous identifiers.
func nodeFingerprint(uid string) string {
	if nodeFingerprintPattern.MatchString(uid) {
		return uid
	}
	return fmt.Sprintf("sha256-%x", sha256.Sum256([]byte("kubememlens-node-identity-v1\x00"+uid)))
}

func redactNode(b *NodeBundle) {
	b.Redacted = true
	e := &b.Evidence
	e.Record.NodeUID = nodeFingerprint(e.Record.NodeUID)
	e.Analysis.NodeUID = e.Record.NodeUID
	for _, o := range []*nodecontext.Observation{e.Record.Report, e.Record.LastGood} {
		if o != nil {
			o.NodeUID = e.Record.NodeUID
		}
	}
	if b.History != nil {
		b.History.Generation = nodeFingerprint(b.History.Generation)
		for i := range b.History.Series {
			s := &b.History.Series[i]
			s.NodeUID = nodeFingerprint(s.NodeUID)
			for j := range s.Points {
				s.Points[j].Observation.NodeUID = s.NodeUID
			}
		}
	}
	if r := e.Analysis.Rankings; r != nil {
		namespaces := map[string]string{}
		redactRows := func(rows []nodeanalysis.Contributor, prefix string) {
			for i := range rows {
				r := &rows[i]
				alias, ok := namespaces[r.Namespace]
				if !ok {
					alias = fmt.Sprintf("namespace-%d", len(namespaces)+1)
					namespaces[r.Namespace] = alias
				}
				r.Namespace = alias
				r.Name = fmt.Sprintf("%s-%d", prefix, i+1)
				r.UID = ""
				if prefix == "workload" {
					r.Kind = "Workload"
				}
			}
		}
		redactRows(r.Pods, "pod")
		redactRows(r.Workloads, "workload")
	}
}
