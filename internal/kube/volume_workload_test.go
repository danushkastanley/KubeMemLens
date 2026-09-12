package kube

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
)

type workloadFixture struct {
	mu                                                                      sync.Mutex
	root, parent                                                            workloadObject
	pods                                                                    []corev1.Pod
	reads, auth                                                             map[string]int
	denied                                                                  map[string]bool
	denyRootAfterFirst, replaceRoot, replacePod, replaceOwner, continuePods bool
	replaceVolumeLimit                                                      bool
	resolver                                                                WorkloadVolumeResolver
}

func workloadOwner(object workloadObject) metav1.OwnerReference {
	yes := true
	return metav1.OwnerReference{APIVersion: object.APIVersion, Kind: object.Kind, Name: object.Name, UID: object.UID, Controller: &yes}
}

func newWorkloadFixture(t *testing.T, kind string) *workloadFixture {
	t.Helper()
	resource, _ := volumeWorkloadResource(kind)
	f := &workloadFixture{reads: map[string]int{}, auth: map[string]int{}, denied: map[string]bool{}}
	f.root = workloadObject{TypeMeta: metav1.TypeMeta{APIVersion: resource.version, Kind: resource.kind}, ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "app", UID: "root-uid"}}
	f.root.Spec.Selector = json.RawMessage(`{"matchLabels":{"app":"fixture"}}`)
	if kind == "ReplicationController" {
		f.root.Spec.Selector = json.RawMessage(`{"app":"fixture"}`)
	}
	owner := f.root
	if kind == "Deployment" || kind == "CronJob" {
		bridge := "ReplicaSet"
		if kind == "CronJob" {
			bridge = "Job"
		}
		r, _ := volumeWorkloadResource(bridge)
		f.parent = workloadObject{TypeMeta: metav1.TypeMeta{APIVersion: r.version, Kind: r.kind}, ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "child", UID: "parent-uid", OwnerReferences: []metav1.OwnerReference{workloadOwner(f.root)}}}
		f.parent.Spec.Selector = f.root.Spec.Selector
		owner = f.parent
	}
	for _, name := range []string{"pod-a", "pod-b"} {
		f.pods = append(f.pods, corev1.Pod{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"}, ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: name, UID: types.UID(name + "-uid"), CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour)), OwnerReferences: []metav1.OwnerReference{workloadOwner(owner)}}, Spec: corev1.PodSpec{NodeName: "node-a", Volumes: []corev1.Volume{{Name: "scratch", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory}}}}, Containers: []corev1.Container{{Name: "app", VolumeMounts: []corev1.VolumeMount{{Name: "scratch", MountPath: "/private"}}}}}})
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.reads[r.URL.Path]++
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer source" {
			t.Error("unexpected acquisition identity or mutation")
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == resource.path("team")+"/app":
			root := f.root
			if f.replaceRoot && f.reads[r.URL.Path] > 1 {
				root.UID = "replaced-root"
			}
			_ = json.NewEncoder(w).Encode(root)
		case f.parent.Name != "" && strings.HasSuffix(r.URL.Path, "/child"):
			_ = json.NewEncoder(w).Encode(f.parent)
		case kind == "CronJob" && r.URL.Path == "/apis/batch/v1/namespaces/team/jobs":
			_ = json.NewEncoder(w).Encode(workloadList{TypeMeta: metav1.TypeMeta{APIVersion: "batch/v1", Kind: "JobList"}, Items: []workloadObject{f.parent}})
		case r.URL.Path == "/api/v1/namespaces/team/pods":
			if r.URL.Query().Get("labelSelector") != "app=fixture" || r.URL.Query().Get("limit") != "33" {
				t.Error("unbounded or unselected Pod list")
			}
			page := corev1.PodList{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PodList"}, Items: f.pods}
			if f.continuePods {
				page.Continue = "next"
			}
			_ = json.NewEncoder(w).Encode(page)
		case strings.HasPrefix(r.URL.Path, "/api/v1/namespaces/team/pods/"):
			for _, original := range f.pods {
				if strings.HasSuffix(r.URL.Path, "/"+original.Name) {
					pod := *original.DeepCopy()
					if f.reads[r.URL.Path] > 1 {
						if f.replaceVolumeLimit {
							limit := resourceapi.MustParse("16Mi")
							pod.Spec.Volumes[0].EmptyDir.SizeLimit = &limit
						}
						if f.replacePod {
							pod.UID = "replacement"
						}
						if f.replaceOwner {
							pod.OwnerReferences[0].UID = "other-owner"
						}
					}
					_ = json.NewEncoder(w).Encode(pod)
					return
				}
			}
			http.NotFound(w, r)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	config := &rest.Config{Host: server.URL, BearerToken: "source", TLSClientConfig: rest.TLSClientConfig{CAData: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})}}
	resolver, err := NewVolumeResolver(config, func(_ context.Context, a VolumeAccess) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		verb := a.Verb
		if verb == "" {
			verb = "get"
		}
		key := a.Resource + "/" + verb
		f.auth[key]++
		if f.denied[key] || (f.denyRootAfterFirst && a.Resource == resource.resource && a.Name == "app" && f.auth[key] > 1) {
			return &HealthReadError{Reason: volumehealth.AccessDenied}
		}
		if a.Namespace != "team" {
			t.Error("authorisation escaped namespace")
		}
		return nil
	}, func(node string, _ time.Time) (string, bool) { return "node-uid", node == "node-a" })
	if err != nil {
		t.Fatal(err)
	}
	f.resolver = resolver.(WorkloadVolumeResolver)
	return f
}

func TestWorkloadVolumeLiveOwnership(t *testing.T) {
	for _, kind := range []string{"Deployment", "ReplicaSet", "StatefulSet", "DaemonSet", "ReplicationController", "Job", "CronJob"} {
		t.Run(kind, func(t *testing.T) {
			f := newWorkloadFixture(t, kind)
			result, err := f.resolver.ResolveWorkload(t.Context(), "team", kind, "app")
			if err != nil || len(result.Pods) != 2 || result.UID != "root-uid" {
				t.Fatalf("resolved workload: %+v %v", result, err)
			}
			if f.auth["pods/list"] != 1 || f.auth["pods/get"] != 4 {
				t.Fatalf("caller permissions were not checked: %v", f.auth)
			}
			if kind == "Deployment" && f.reads["/apis/apps/v1/namespaces/team/replicasets/child"] != 2 {
				t.Fatal("parent was not bounded to initial/final reads")
			}
		})
	}
}

func TestWorkloadVolumeRevocationReplacementAndBounds(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*workloadFixture)
	}{
		{"list denied", func(f *workloadFixture) { f.denied["pods/list"] = true }},
		{"root denied", func(f *workloadFixture) { f.denied["deployments/get"] = true }},
		{"parent denied", func(f *workloadFixture) { f.denied["replicasets/get"] = true }},
		{"root revoked", func(f *workloadFixture) { f.denyRootAfterFirst = true }},
		{"root replaced", func(f *workloadFixture) { f.replaceRoot = true }},
		{"Pod replaced", func(f *workloadFixture) { f.replacePod = true }},
		{"owner replaced", func(f *workloadFixture) { f.replaceOwner = true }},
		{"incomplete page", func(f *workloadFixture) { f.continuePods = true }},
		{"duplicate UID", func(f *workloadFixture) { f.pods[1].UID = f.pods[0].UID }},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newWorkloadFixture(t, "Deployment")
			test.change(f)
			result, err := f.resolver.ResolveWorkload(t.Context(), "team", "Deployment", "app")
			if err == nil || len(result.Pods) != 0 {
				t.Fatal("unsafe or incomplete ownership accepted")
			}
			if strings.Contains(err.Error(), "root-uid") || strings.Contains(err.Error(), "pod-a") {
				t.Fatal("identity in error")
			}
			if test.name == "incomplete page" && !errors.Is(err, ErrVolumeWorkloadBounds) {
				t.Fatal("coverage bound not explicit")
			}
		})
	}
}

func TestWorkloadVolumeKeepsScheduledEvidenceBesidePendingPods(t *testing.T) {
	f := newWorkloadFixture(t, "Deployment")
	f.pods[1].Spec.NodeName = ""
	result, err := f.resolver.ResolveWorkload(t.Context(), "team", "Deployment", "app")
	if err != nil || len(result.Pods) != 1 || len(result.Unscheduled) != 1 || result.Unscheduled[0].PodName != "pod-b" {
		t.Fatalf("pending Pod erased other evidence: %+v %v", result, err)
	}
}

func TestWorkloadVolumeSizeLimitFormattingDoesNotChangeBinding(t *testing.T) {
	f := newWorkloadFixture(t, "Deployment")
	limit := resourceapi.MustParse("8Mi")
	f.pods[0].Spec.Volumes[0].EmptyDir.SizeLimit = &limit
	value, err := f.resolver.ResolveWorkload(context.Background(), "team", "Deployment", "app")
	if err != nil || len(value.Pods) != 2 {
		t.Fatal("quantity formatting broke unchanged binding", err)
	}
	if value.Pods[0].Bindings[0].Configuration.SizeLimitBytes == nil || *value.Pods[0].Bindings[0].Configuration.SizeLimitBytes != 8<<20 {
		t.Fatal("configured limit lost")
	}
}

func TestWorkloadRejectsChangedVolumeSizeLimit(t *testing.T) {
	f := newWorkloadFixture(t, "Deployment")
	limit := resourceapi.MustParse("8Mi")
	f.pods[0].Spec.Volumes[0].EmptyDir.SizeLimit = &limit
	f.replaceVolumeLimit = true
	if _, err := f.resolver.ResolveWorkload(context.Background(), "team", "Deployment", "app"); err == nil {
		t.Fatal("changed volume size limit accepted")
	}
}
