package kube

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/fake"
)

func TestPodCacheFiltersNodeAndTracksWatchUpdates(t *testing.T) {
	client := fake.NewSimpleClientset(
		cachePod("pod-a", "uid-a", "node-a", "id-a"),
		cachePod("pod-b", "uid-b", "node-b", "id-b"),
	)
	podCache := NewPodCache(client, "node-a")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go podCache.Run(ctx)

	syncCtx, syncCancel := context.WithTimeout(ctx, 5*time.Second)
	defer syncCancel()
	if !podCache.WaitForSync(syncCtx) {
		t.Fatal("pod cache did not sync")
	}
	if podCache.PodCount() != 1 {
		t.Fatalf("PodCount = %d, want 1", podCache.PodCount())
	}
	if ref, ok := podCache.Index().Lookup("id-a", ""); !ok || ref.PodName != "pod-a" {
		t.Fatalf("node-a lookup failed: %#v ok=%v", ref, ok)
	}
	if _, ok := podCache.Index().Lookup("id-b", ""); ok {
		t.Fatal("cache included pod from node-b")
	}

	if _, err := client.CoreV1().Pods("default").Create(ctx, cachePod("pod-c", "uid-c", "node-a", "id-c"), metav1.CreateOptions{}); err != nil {
		t.Fatalf("create pod: %v", err)
	}
	if err := wait.PollUntilContextTimeout(ctx, 10*time.Millisecond, 5*time.Second, true, func(context.Context) (bool, error) {
		_, ok := podCache.Index().Lookup("id-c", "")
		return ok, nil
	}); err != nil {
		t.Fatalf("watch update did not reach cache: %v", err)
	}
}

func cachePod(name, uid, node, containerID string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      name,
			UID:       types.UID(uid),
		},
		Spec: corev1.PodSpec{NodeName: node},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
			{Name: "app", ContainerID: "containerd://" + containerID},
		}},
	}
}
