package agentless

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
)

func TestClusterNodesPageWithinBoundAndDiscardOnDenial(t *testing.T) {
	for _, outcome := range []string{"complete", "capped", "denied", "duplicate"} {
		t.Run(outcome, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/nodes" || r.URL.Query().Get("limit") != "1" {
					t.Errorf("unexpected node read: %s", r.URL)
				}
				name, cursor := "node-a", "next"
				if r.URL.Query().Get("continue") == "next" {
					if outcome == "denied" {
						w.WriteHeader(403)
						return
					}
					name, cursor = "node-b", ""
					if outcome == "duplicate" {
						name = "node-a"
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(corev1.NodeList{TypeMeta: metav1.TypeMeta{Kind: "NodeList", APIVersion: "v1"}, ListMeta: metav1.ListMeta{Continue: cursor, ResourceVersion: "1"}, Items: []corev1.Node{{ObjectMeta: metav1.ObjectMeta{Name: name, UID: types.UID(name)}}}})
			}))
			defer server.Close()
			maxNodes := 5
			if outcome == "capped" {
				maxNodes = 1
			}
			reader, err := NewCluster(&rest.Config{Host: server.URL}, Options{PageSize: 1, MaxNodes: maxNodes})
			if err != nil {
				t.Fatal(err)
			}
			nodes, report := reader.readNodes(withReadBudget(t.Context()), nil, time.Now())
			if outcome == "complete" {
				if len(nodes) != 2 || report.Completeness != capability.Complete {
					t.Fatalf("%+v %+v", nodes, report)
				}
			} else if len(nodes) != 0 || report.Completeness != capability.Partial {
				t.Fatalf("retained incomplete node list: %+v %+v", nodes, report)
			}
		})
	}
}

func TestNodeResourcesRejectOverflowAndPreserveKnownZero(t *testing.T) {
	node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a", UID: "uid"}, Status: corev1.NodeStatus{Capacity: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("0"), "hugepages-2Mi": resource.MustParse("8Mi")}}}
	row, err := nodeMetadata(node, time.Now())
	if err != nil || row.CapacityMemoryBytes == nil || *row.CapacityMemoryBytes != 0 || row.AllocatableMemoryBytes != nil || row.HugepageCapacity["hugepages-2Mi"] != 8<<20 || row.MemoryPressure != "unreported" {
		t.Fatalf("%+v %v", row, err)
	}
	for _, quantity := range []string{"-1", "9223372036854775808"} {
		node.Status.Capacity[corev1.ResourceMemory] = resource.MustParse(quantity)
		if _, err := nodeMetadata(node, time.Now()); err == nil {
			t.Fatalf("accepted %s", quantity)
		}
	}
}
