package kube

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHealthRejectsUntrustedIdentityAndLimits(t *testing.T) {
	for name, change := range map[string]func(healthFixture){
		"pod namespace":                   func(f healthFixture) { f.pod.Namespace = "team-b" },
		"claim same name other namespace": func(f healthFixture) { f.pvc.Namespace = "team-b" },
		"missing pod UID":                 func(f healthFixture) { f.pod.UID = "" },
		"claim path traversal":            func(f healthFixture) { f.pod.Spec.Volumes[0].PersistentVolumeClaim.ClaimName = "../team-b/data" },
		"PV namespace":                    func(f healthFixture) { f.pv.Namespace = "team-b" },
		"wrong node":                      func(f healthFixture) { f.node.Name = "node-b" },
		"duplicate volumes":               func(f healthFixture) { f.pod.Spec.Volumes = append(f.pod.Spec.Volumes, f.pod.Spec.Volumes[0]) },
		"volume ceiling":                  func(f healthFixture) { f.pod.Spec.Volumes = make([]corev1.Volume, 65) },
		"unmatched Pod report":            func(f healthFixture) { f.pod.Status.VolumeHealth = []corev1.PodVolumeHealth{{Name: "not-in-spec"}} },
		"duplicate Pod report": func(f healthFixture) {
			f.pod.Status.VolumeHealth = []corev1.PodVolumeHealth{{Name: "data"}, {Name: "data"}}
		},
		"Pod condition ceiling": func(f healthFixture) {
			f.pod.Status.VolumeHealth = []corev1.PodVolumeHealth{{Name: "data", HealthConditions: make([]corev1.VolumeHealthCondition, 17)}}
		},
		"PVC condition ceiling": func(f healthFixture) {
			f.pvc.Status.HealthStatus = &corev1.VolumeHealthStatus{HealthConditions: make([]corev1.VolumeHealthCondition, 17)}
		},
		"empty condition status": func(f healthFixture) {
			f.pvc.Status.HealthStatus = &corev1.VolumeHealthStatus{HealthConditions: []corev1.VolumeHealthCondition{{}}}
		},
		"backend duplicate report": func(f healthFixture) {
			f.node.Status.StorageHealth = []storagev1.StorageHealth{{Name: "storage.example.test"}, {Name: "storage.example.test"}}
		},
		"backend condition ceiling": func(f healthFixture) {
			f.node.Status.StorageHealth = []storagev1.StorageHealth{{Name: "storage.example.test", HealthConditions: make([]storagev1.StorageHealthCondition, 17)}}
		},
		"driver ceiling": func(f healthFixture) { f.node.Spec.Drivers = make([]storagev1.CSINodeDriver, 129) },
	} {
		t.Run(name, func(t *testing.T) {
			f := newHealthFixture()
			change(f)
			r, err := testHealthReader(t, f.handler(t, ""), VolumeHealthOptions{}).Query(context.Background(), "workload")
			if err == nil || r.Summary().Observations != 0 {
				t.Fatalf("invalid query retained data: %v %v", r, err)
			}
		})
	}
}

func TestHealthBindingAndDriverAvailability(t *testing.T) {
	for name, change := range map[string]func(healthFixture){
		"PVC recreated":                 func(f healthFixture) { f.pv.Spec.ClaimRef.UID = "previous-claim" },
		"same name different namespace": func(f healthFixture) { f.pv.Spec.ClaimRef.Namespace = "team-b" },
		"unbound":                       func(f healthFixture) { f.pvc.Spec.VolumeName = "" },
		"no claim reference":            func(f healthFixture) { f.pv.Spec.ClaimRef = nil },
		"not CSI":                       func(f healthFixture) { f.pv.Spec.CSI = nil },
		"unregistered driver":           func(f healthFixture) { f.node.Spec.Drivers = nil },
	} {
		t.Run(name, func(t *testing.T) {
			f := newHealthFixture()
			change(f)
			f.node.Status.StorageHealth = []storagev1.StorageHealth{{Name: "storage.example.test"}}
			r, err := testHealthReader(t, f.handler(t, ""), VolumeHealthOptions{}).Query(context.Background(), "workload")
			if err != nil {
				t.Fatal(err)
			}
			backend := r.Interactive()[2]
			if backend.Availability == volumehealth.Reported || backend.State == volumehealth.StateHealthy {
				t.Fatalf("unverified join became healthy: %v", r)
			}
			if name == "not CSI" && backend.Availability != volumehealth.Unsupported {
				t.Fatal("non-CSI volume not distinguished")
			}
		})
	}
}

func TestGenericEphemeralClaimRequiresPodOwnership(t *testing.T) {
	for _, owned := range []bool{false, true} {
		f := newHealthFixture()
		f.pod.Spec.Volumes[0].PersistentVolumeClaim = nil
		f.pod.Spec.Volumes[0].Ephemeral = &corev1.EphemeralVolumeSource{}
		f.pvc.Name = "workload-data"
		f.pvc.Spec.VolumeName = ""
		f.pvc.Status.HealthStatus = &corev1.VolumeHealthStatus{}
		if owned {
			yes := true
			f.pvc.OwnerReferences = []metav1.OwnerReference{{APIVersion: "v1", Kind: "Pod", Name: f.pod.Name, UID: f.pod.UID, Controller: &yes}}
		}
		h := f.handler(t, "")
		reader := testHealthReader(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/namespaces/team-a/persistentvolumeclaims/workload-data" {
				if err := json.NewEncoder(w).Encode(f.pvc); err != nil {
					t.Error(err)
				}
				return
			}
			h(w, r)
		}, VolumeHealthOptions{})
		r, err := reader.Query(context.Background(), "workload")
		if err != nil {
			t.Fatal(err)
		}
		if (r.Interactive()[1].State == volumehealth.StateHealthy) != owned {
			t.Fatal("ephemeral ownership not enforced")
		}
	}
}

func TestInlineCSIAndUnscheduledPod(t *testing.T) {
	f := newHealthFixture()
	f.pod.Spec.Volumes[0].PersistentVolumeClaim = nil
	f.pod.Spec.Volumes[0].CSI = &corev1.CSIVolumeSource{Driver: "storage.example.test"}
	f.pod.Spec.NodeName = ""
	r, err := testHealthReader(t, f.handler(t, ""), VolumeHealthOptions{}).Query(context.Background(), "workload")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Interactive()) != 2 || r.Interactive()[1].Reason != volumehealth.NotScheduled {
		t.Fatalf("unscheduled inline report: %v", r)
	}
}
