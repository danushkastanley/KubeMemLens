package kube

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/fake"
)

func TestHealthInformerTracksStatusWithoutBypassingAuthorisation(t *testing.T) {
	f := newHealthFixture()
	other := f.pod.DeepCopy()
	other.Namespace = "team-b"
	other.UID = "pod-b"
	client := fake.NewSimpleClientset(f.pod, other)
	cache := NewPodCache(client, "node-a")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go cache.Run(ctx)
	if !cache.WaitForSync(ctx) {
		t.Fatal("cache did not sync")
	}
	f.pod = f.pod.DeepCopy()
	f.pod.ResourceVersion = "2"
	f.pod.Status.VolumeHealth = []corev1.PodVolumeHealth{{Name: "data", HealthConditions: []corev1.VolumeHealthCondition{{Status: "Degraded", Message: "authorised-message"}}}}
	if _, err := client.CoreV1().Pods("team-a").UpdateStatus(ctx, f.pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := wait.PollUntilContextTimeout(ctx, 10*time.Millisecond, time.Second, true, func(context.Context) (bool, error) {
		obj, ok, _ := cache.informer.GetStore().GetByKey("team-a/workload")
		return ok && obj.(*corev1.Pod).ResourceVersion == "2", nil
	}); err != nil {
		t.Fatal(err)
	}
	h := f.handler(t, "")
	var revoked atomic.Bool
	reader := testHealthReader(t, func(w http.ResponseWriter, r *http.Request) {
		if revoked.Load() {
			http.Error(w, "denied", 403)
			return
		}
		h(w, r)
	}, VolumeHealthOptions{PodCache: cache})
	r, err := reader.Query(ctx, "workload")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Interactive()[0].Adverse || r.Interactive()[0].Identity.PodUID != "pod-a" {
		t.Fatal("informer health lost or joined other namespace")
	}
	copy := cache.volumeHealthPod(*f.pod)
	copy.Status.VolumeHealth[0].HealthConditions[0].Message = "mutated"
	if cache.volumeHealthPod(*f.pod).Status.VolumeHealth[0].HealthConditions[0].Message != "authorised-message" {
		t.Fatal("cache leaked mutable status")
	}
	for _, change := range []func(*corev1.Pod){func(p *corev1.Pod) { p.UID = "recreated" }, func(p *corev1.Pod) { p.ResourceVersion = "3" }, func(p *corev1.Pod) { p.Spec.NodeName = "node-b" }} {
		live := f.pod.DeepCopy()
		change(live)
		live.Status.VolumeHealth = nil
		if len(cache.volumeHealthPod(*live).Status.VolumeHealth) != 0 {
			t.Fatal("stale informer crossed live identity/version")
		}
	}
	revoked.Store(true)
	r, err = reader.Query(ctx, "workload")
	if err == nil || r.Summary().Observations != 0 {
		t.Fatal("informer bypassed revoked permission")
	}
}
