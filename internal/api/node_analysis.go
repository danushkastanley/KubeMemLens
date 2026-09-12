package api

import (
	"github.com/danushkastanley/kube-memlens/internal/nodeanalysis"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type NodeMemoryAnalysis struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Analysis          nodeanalysis.Analysis `json:"analysis"`
}
