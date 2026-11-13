package kube

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// The scanner needs to recognise a Pod's cgroup before any of its containers
// has an ID, so every Pod on the node is indexed by the identities that can
// appear in its cgroup path.
func TestPodIndexRecordsEveryPodIncludingThoseWithoutContainers(t *testing.T) {
	index := BuildPodIndexFromPods([]corev1.Pod{
		{ObjectMeta: metav1.ObjectMeta{Namespace: "shop", Name: "pulling", UID: "AAAAAAAA-1111-2222-3333-444444444444"}},
		{
			ObjectMeta: metav1.ObjectMeta{Namespace: "shop", Name: "running", UID: "bbbbbbbb-1111-2222-3333-444444444444"},
			Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
				Name: "app", ContainerID: "containerd://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
			}}},
		},
		{ObjectMeta: metav1.ObjectMeta{
			Namespace: "kube-system", Name: "static", UID: "cccccccc-1111-2222-3333-444444444444",
			Annotations: map[string]string{"kubernetes.io/config.hash": "c2df6c2eedc06b1195677a320a366f5e"},
		}},
	})
	for identity, podUID := range map[string]string{
		"aaaaaaaa-1111-2222-3333-444444444444": "aaaaaaaa-1111-2222-3333-444444444444",
		"bbbbbbbb-1111-2222-3333-444444444444": "bbbbbbbb-1111-2222-3333-444444444444",
		"cccccccc-1111-2222-3333-444444444444": "cccccccc-1111-2222-3333-444444444444",
		// A static Pod's cgroup is named after its configuration hash.
		"c2df6c2eedc06b1195677a320a366f5e": "cccccccc-1111-2222-3333-444444444444",
	} {
		if got, known := index.PodUIDs[identity]; !known || got != podUID {
			t.Fatalf("Pod identity %s maps to %q, want %q", identity, got, podUID)
		}
	}
	if len(index.PodUIDs) != 4 || len(index.ByPodUID) != 1 {
		t.Fatalf("PodUIDs=%d ByPodUID=%d", len(index.PodUIDs), len(index.ByPodUID))
	}
}
