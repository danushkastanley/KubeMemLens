package api

import (
	"github.com/danushkastanley/kube-memlens/internal/memorytopology"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const TopologySnapshotSchemaVersion = 7

// NodeMemoryTopology is served only by the named, separately authorised Node
// endpoint. Ordinary Node snapshots and history never contain this evidence.
type NodeMemoryTopology struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Current           *memorytopology.Report `json:"current,omitempty"`
	LastGood          *memorytopology.Report `json:"lastGood,omitempty"`
}

func (r NodeMemoryTopology) Validate(name string) error {
	if r.Current == nil && r.LastGood != nil {
		return memorytopology.ErrInvalid
	}
	if r.APIVersion != MemoryAPIGroup+"/"+MemoryAPIVersion || r.Kind != "NodeMemoryTopology" || r.Name != name || r.Namespace != "" {
		return memorytopology.ErrInvalid
	}
	uid := ""
	for _, report := range []*memorytopology.Report{r.Current, r.LastGood} {
		if report == nil {
			continue
		}
		if err := report.Validate(); err != nil {
			return err
		}
		if report.Observation.NodeName != name || (uid != "" && uid != report.Observation.NodeUID) {
			return memorytopology.ErrInvalid
		}
		uid = report.Observation.NodeUID
	}
	return nil
}
