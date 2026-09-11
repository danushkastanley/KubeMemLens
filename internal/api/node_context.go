package api

import (
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NodeContextRecord contains only Node evidence. Contributor data and residual
// arithmetic require a separate cluster-wide Pod authorisation decision.
type NodeContextRecord struct {
	NodeName   string                   `json:"nodeName"`
	NodeUID    string                   `json:"nodeUID"`
	ReceivedAt time.Time                `json:"receivedAt,omitzero"`
	Freshness  capability.Freshness     `json:"freshness"`
	Report     *nodecontext.Observation `json:"report,omitempty"`
	LastGood   *nodecontext.Observation `json:"lastGood,omitempty"`
}

type NodeContextResource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Record            NodeContextRecord `json:"record"`
}

type NodeContextList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodeContextResource `json:"items"`
}

type NodeContextHistoryPoint struct {
	ReceivedAt  time.Time               `json:"receivedAt"`
	Observation nodecontext.Observation `json:"observation"`
}

type NodeContextHistorySeries struct {
	NodeUID string                    `json:"nodeUID"`
	Points  []NodeContextHistoryPoint `json:"points"`
}

type NodeContextHistory struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	NodeName        string                     `json:"nodeName"`
	Generation      string                     `json:"generation"`
	ResetAt         time.Time                  `json:"resetAt"`
	WindowSeconds   int64                      `json:"windowSeconds"`
	Completeness    capability.Completeness    `json:"completeness"`
	CoverageLost    bool                       `json:"coverageLost"`
	Series          []NodeContextHistorySeries `json:"series"`
}

type NodeContextDebug struct {
	SharedNodeRecords int  `json:"sharedNodeRecords"`
	Records           int  `json:"records"`
	FreshRecords      int  `json:"freshRecords"`
	StaleRecords      int  `json:"staleRecords"`
	FailedRecords     int  `json:"failedRecords"`
	MaxRecords        int  `json:"maxRecords"`
	HistorySeries     int  `json:"historySeries"`
	HistoryPoints     int  `json:"historyPoints"`
	HistoryBytes      int  `json:"historyBytes"`
	MaxHistorySeries  int  `json:"maxHistorySeries"`
	MaxHistoryPoints  int  `json:"maxHistoryPoints"`
	MaxHistoryBytes   int  `json:"maxHistoryBytes"`
	CoverageLost      bool `json:"coverageLost"`
}
