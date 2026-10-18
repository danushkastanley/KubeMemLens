package kube

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestMaximumMarkerCohortFitsAcquisitionAndDisclosureBudgets(t *testing.T) {
	for _, distinctParents := range []bool{false, true} {
		t.Run(fmt.Sprintf("distinct-parents-%t", distinctParents), func(t *testing.T) {
			f := newMarkerFixture(t)
			f.selection.Request = memoryhistory.Request{Scope: memoryhistory.Workload, Namespace: "tenant-a", Name: "app", WorkloadKind: "Deployment"}
			f.selection.UID = "deployment-uid"
			f.selection.Targets = nil
			f.events = nil
			f.extra = map[string]any{}
			controller := true
			pods := corev1.PodList{TypeMeta: metav1.TypeMeta{Kind: "PodList", APIVersion: "v1"}}
			for i := 0; i < memoryhistory.MaxTargets; i++ {
				pod := f.pod.DeepCopy()
				pod.Name = fmt.Sprintf("app-%02d", i)
				pod.UID = types.UID(fmt.Sprintf("pod-%02d", i))
				if distinctParents {
					rs := f.rs
					rs.ObjectMeta = *f.rs.ObjectMeta.DeepCopy()
					rs.Name = fmt.Sprintf("app-rs-%02d", i)
					rs.UID = types.UID(fmt.Sprintf("rs-%02d", i))
					f.extra["/apis/apps/v1/namespaces/tenant-a/replicasets/"+rs.Name] = rs
					pod.OwnerReferences = []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: &controller}}
				}
				f.extra["/api/v1/namespaces/tenant-a/pods/"+pod.Name] = pod
				pods.Items = append(pods.Items, *pod)
				f.selection.Targets = append(f.selection.Targets, memoryhistory.Target{Namespace: pod.Namespace, Pod: pod.Name, PodUID: string(pod.UID), Container: "app", ContainerID: "containerd://current", Node: "node-a", NodeUID: "node-uid", StartedAt: f.now.Add(-3 * time.Minute), PodCreatedAt: f.now.Add(-4 * time.Minute)})
			}
			f.extra["/api/v1/namespaces/tenant-a/pods"] = pods
			start := time.Now()
			report, err := f.provider.Query(t.Context(), f.selection, f.query)
			if err != nil {
				t.Fatal("maximum cohort acquisition", err)
			}
			queried := time.Now()
			if err := f.provider.Revalidate(t.Context(), report); err != nil {
				t.Fatal("maximum cohort disclosure", err)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			t.Logf("acquisition=%s revalidation=%s HTTP reads=%d authorisations=%d markers=%d", queried.Sub(start), time.Since(queried), len(f.reads), len(f.access), len(report.Markers))
		})
	}
}

func maximumCronJobMarkerFixture(t *testing.T) *markerFixture {
	t.Helper()
	f := newMarkerFixtureAt(t, time.Now().UTC().Truncate(time.Second))
	root := f.deployment
	root.TypeMeta = metav1.TypeMeta{Kind: "CronJob", APIVersion: "batch/v1"}
	root.Name = "cron"
	root.UID = "cron-uid"
	root.Annotations = nil
	f.selection.Request = memoryhistory.Request{Scope: memoryhistory.Workload, Namespace: "tenant-a", Name: root.Name, WorkloadKind: "CronJob"}
	f.selection.UID = string(root.UID)
	f.selection.Targets = nil
	f.extra = map[string]any{"/apis/batch/v1/namespaces/tenant-a/cronjobs/cron": root}
	f.events = nil
	jobs := workloadList{TypeMeta: metav1.TypeMeta{Kind: "JobList", APIVersion: "batch/v1"}}
	controller := true
	for i := 0; i < memoryhistory.MaxTargets; i++ {
		job := f.rs
		job.ObjectMeta = *f.rs.ObjectMeta.DeepCopy()
		job.TypeMeta = metav1.TypeMeta{Kind: "Job", APIVersion: "batch/v1"}
		job.Name = fmt.Sprintf("job-%02d", i)
		job.UID = types.UID(job.Name + "-uid")
		job.Annotations = nil
		job.OwnerReferences = []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "CronJob", Name: root.Name, UID: root.UID, Controller: &controller}}
		job.Spec.Selector = json.RawMessage(fmt.Sprintf(`{"matchLabels":{"job":"%s"}}`, job.Name))
		jobs.Items = append(jobs.Items, job)
		f.extra["/apis/batch/v1/namespaces/tenant-a/jobs/"+job.Name] = job
		pod := *f.pod.DeepCopy()
		pod.Name = fmt.Sprintf("pod-%02d", i)
		pod.UID = types.UID(pod.Name + "-uid")
		pod.OwnerReferences = []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: job.Name, UID: job.UID, Controller: &controller}}
		f.extra["/api/v1/namespaces/tenant-a/pods?job="+job.Name] = corev1.PodList{TypeMeta: metav1.TypeMeta{Kind: "PodList", APIVersion: "v1"}, Items: []corev1.Pod{pod}}
		f.extra["/api/v1/namespaces/tenant-a/pods/"+pod.Name] = pod
		f.selection.Targets = append(f.selection.Targets, memoryhistory.Target{Namespace: pod.Namespace, Pod: pod.Name, PodUID: string(pod.UID), Container: "app", ContainerID: "containerd://current", Node: "node-a", NodeUID: "node-uid", StartedAt: f.now.Add(-3 * time.Minute), PodCreatedAt: f.now.Add(-4 * time.Minute)})
	}
	f.extra["/apis/batch/v1/namespaces/tenant-a/jobs"] = jobs
	return f
}

func TestMaximumCronJobMarkerCohortFitsBothPhaseBudgets(t *testing.T) {
	f := maximumCronJobMarkerFixture(t)
	start := time.Now()
	report, err := f.provider.Query(t.Context(), f.selection, f.query)
	if err != nil {
		t.Fatal("CronJob acquisition", err)
	}
	queried := time.Now()
	if err := f.provider.Revalidate(t.Context(), report); err != nil {
		t.Fatal("CronJob revalidation", err)
	}
	t.Logf("acquisition=%s revalidation=%s", queried.Sub(start), time.Since(queried))
}
