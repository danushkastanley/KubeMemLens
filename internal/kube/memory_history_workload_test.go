package kube

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func historyWorkloadFixture(t *testing.T, kind string) (*workloadFixture, *memoryHistoryResolver) {
	t.Helper()
	f := newWorkloadFixture(t, kind)
	f.root.Generation = 4
	for i := range f.pods {
		f.pods[i].Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "app", ContainerID: "containerd://" + f.pods[i].Name, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(f.pods[i].CreationTimestamp.Add(time.Minute))}}}}
	}
	v := f.resolver.(*volumeResolver)
	return f, &memoryHistoryResolver{reader: v.reader, authorize: v.authorize, nodeIdentity: v.nodeUID, calls: v.calls, gate: make(chan struct{}, 1), subresources: []string{"trends"}, workload: (*volumeBindingQuery).historyWorkload}
}

func TestHistoryWorkloadResolvesEverySupportedOwnerChain(t *testing.T) {
	for _, kind := range []string{"Deployment", "ReplicaSet", "StatefulSet", "DaemonSet", "ReplicationController", "Job", "CronJob"} {
		t.Run(kind, func(t *testing.T) {
			f, resolver := historyWorkloadFixture(t, kind)
			s, err := resolver.Resolve(t.Context(), memoryhistory.Request{Scope: memoryhistory.Workload, Namespace: "team", Name: "app", WorkloadKind: kind})
			if err != nil || s.UID != "root-uid" || s.Revision != "4" || len(s.Targets) != 2 {
				t.Fatalf("selection=%+v error=%v", s, err)
			}
			for path := range f.reads {
				if strings.Contains(path, "persistentvolume") || strings.Contains(path, "csinode") {
					t.Fatal("history acquired volume resources")
				}
			}
			data, _ := json.Marshal(s)
			if strings.Contains(string(data), "/private") {
				t.Fatal("Pod mount metadata leaked into history")
			}
		})
	}
}

func TestHistoryWorkloadRejectsRevocationReplacementAndTruncation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*workloadFixture)
	}{
		{"revoked controller", func(f *workloadFixture) { f.denyRootAfterFirst = true }},
		{"replaced controller", func(f *workloadFixture) { f.replaceRoot = true }},
		{"replaced Pod", func(f *workloadFixture) { f.replacePod = true; f.reads["/api/v1/namespaces/team/pods/pod-a"] = 1 }},
		{"changed owner", func(f *workloadFixture) { f.replaceOwner = true; f.reads["/api/v1/namespaces/team/pods/pod-a"] = 1 }},
		{"partial member list", func(f *workloadFixture) { f.continuePods = true }},
		{"denied Pod", func(f *workloadFixture) { f.denied["pods/get"] = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, resolver := historyWorkloadFixture(t, "Deployment")
			tc.change(f)
			s, err := resolver.Resolve(t.Context(), memoryhistory.Request{Scope: memoryhistory.Workload, Namespace: "team", Name: "app", WorkloadKind: "Deployment"})
			if err == nil || len(s.Targets) != 0 {
				t.Fatal("unconfirmed cohort returned")
			}
		})
	}
}

func TestHistoryWorkloadRejectsMoreThanSixteenCurrentTargets(t *testing.T) {
	f, resolver := historyWorkloadFixture(t, "Deployment")
	prototype := f.pods[0]
	f.pods = nil
	for i := 0; i < memoryhistory.MaxTargets+1; i++ {
		p := prototype.DeepCopy()
		p.Name = fmt.Sprintf("pod-%02d", i)
		p.UID = types.UID(p.Name + "-uid")
		p.Status.ContainerStatuses[0].ContainerID = "containerd://" + p.Name
		f.pods = append(f.pods, *p)
	}
	if _, err := resolver.Resolve(t.Context(), memoryhistory.Request{Scope: memoryhistory.Workload, Namespace: "team", Name: "app", WorkloadKind: "Deployment"}); err != memoryhistory.ErrBounds {
		t.Fatalf("target ceiling: %v", err)
	}
}
