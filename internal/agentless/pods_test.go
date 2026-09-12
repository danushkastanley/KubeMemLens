package agentless

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
)

func pendingPod(namespace, name string) corev1.Pod {
	return corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name, UID: types.UID(namespace + "-" + name), CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Minute))},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "worker"}}}, Status: corev1.PodStatus{Phase: corev1.PodPending}}
}

func podListResponse(t *testing.T, w http.ResponseWriter, cursor string, pods ...corev1.Pod) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	list := corev1.PodList{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PodList"}, ListMeta: metav1.ListMeta{ResourceVersion: "123", Continue: cursor}, Items: pods}
	if err := json.NewEncoder(w).Encode(list); err != nil {
		t.Error(err)
	}
}

func TestPodInventoryIncludesPendingPodsAndIsPagedAndBounded(t *testing.T) {
	for _, cap := range []int{1, 5} {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/namespaces/team-a/pods" || r.URL.Query().Get("limit") != "1" {
				t.Errorf("unexpected request %s", r.URL)
			}
			calls++
			if calls == 1 {
				podListResponse(t, w, "next", pendingPod("team-a", "one"))
				return
			}
			if r.URL.Query().Get("continue") != "next" {
				t.Error("cursor missing")
			}
			podListResponse(t, w, "", pendingPod("team-a", "two"))
		}))
		reader, err := NewNamespace(&rest.Config{Host: server.URL}, "team-a", Options{PageSize: 1, MaxPods: cap})
		if err != nil {
			t.Fatal(err)
		}
		inventory, err := reader.readPods(withReadBudget(t.Context()))
		server.Close()
		if err != nil || calls != 2 {
			t.Fatalf("%+v %v calls=%d", inventory, err, calls)
		}
		if cap == 1 && (len(inventory.pods) != 1 || inventory.reason != limitReached) {
			t.Fatalf("%+v", inventory)
		}
		if cap == 5 && (len(inventory.pods) != 2 || inventory.completeness != capability.Complete || len(inventory.pods[0].Status.ContainerStatuses) != 0) {
			t.Fatalf("%+v", inventory)
		}
	}
}

func TestPodInventoryDiscardsEarlierPagesOnDenialOrScopeViolation(t *testing.T) {
	for _, denial := range []bool{false, true} {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if calls == 1 {
				podListResponse(t, w, "next", pendingPod("team-a", "one"))
				return
			}
			if denial {
				http.Error(w, "private-namespace-details", 403)
				return
			}
			podListResponse(t, w, "", pendingPod("other-tenant", "two"))
		}))
		reader, err := NewNamespace(&rest.Config{Host: server.URL}, "team-a", Options{})
		if err != nil {
			t.Fatal(err)
		}
		inventory, err := reader.readPods(withReadBudget(t.Context()))
		server.Close()
		var queryErr *capability.SelectionError
		if !errors.As(err, &queryErr) || len(inventory.pods) != 0 {
			t.Fatalf("%+v %v", inventory, err)
		}
		if strings.Contains(err.Error(), "private-namespace") {
			t.Fatal("raw API error escaped")
		}
		if denial && queryErr.Reason != capability.AccessDenied {
			t.Fatal(err)
		}
	}
}
