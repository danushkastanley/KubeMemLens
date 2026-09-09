package kube

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

const healthPodPath = "/api/v1/namespaces/team-a/pods/workload"
const healthClaimPath = "/api/v1/namespaces/team-a/persistentvolumeclaims/data"
const healthPVPath = "/api/v1/persistentvolumes/pv-a"
const healthNodePath = "/apis/storage.k8s.io/v1/csinodes/node-a"

type healthFixture struct {
	pod  *corev1.Pod
	pvc  *corev1.PersistentVolumeClaim
	pv   *corev1.PersistentVolume
	node *storagev1.CSINode
}

func newHealthFixture() healthFixture {
	return healthFixture{
		pod:  &corev1.Pod{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"}, ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "workload", UID: "pod-a", ResourceVersion: "1"}, Spec: corev1.PodSpec{NodeName: "node-a", Volumes: []corev1.Volume{{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}}}}}},
		pvc:  &corev1.PersistentVolumeClaim{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PersistentVolumeClaim"}, ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "data", UID: "claim-a"}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "pv-a"}},
		pv:   &corev1.PersistentVolume{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PersistentVolume"}, ObjectMeta: metav1.ObjectMeta{Name: "pv-a", UID: "pv-uid"}, Spec: corev1.PersistentVolumeSpec{ClaimRef: &corev1.ObjectReference{Namespace: "team-a", Name: "data", UID: "claim-a"}, PersistentVolumeSource: corev1.PersistentVolumeSource{CSI: &corev1.CSIPersistentVolumeSource{Driver: "storage.example.test", VolumeHandle: "private-handle"}}}},
		node: &storagev1.CSINode{TypeMeta: metav1.TypeMeta{APIVersion: "storage.k8s.io/v1", Kind: "CSINode"}, ObjectMeta: metav1.ObjectMeta{Name: "node-a", UID: "node-uid"}, Spec: storagev1.CSINodeSpec{Drivers: []storagev1.CSINodeDriver{{Name: "storage.example.test", NodeID: "private-node-id"}}}},
	}
}

func (f healthFixture) handler(t *testing.T, denied string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer caller" {
			t.Error("caller identity or read-only method lost")
		}
		if r.URL.Path == denied {
			http.Error(w, "private provider detail", 403)
			return
		}
		objects := map[string]any{healthPodPath: f.pod, healthClaimPath: f.pvc, healthPVPath: f.pv, healthNodePath: f.node}
		obj, ok := objects[r.URL.Path]
		if !ok {
			t.Errorf("unexpected object read: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if err := json.NewEncoder(w).Encode(obj); err != nil {
			t.Error(err)
		}
	}
}

func testHealthReader(t *testing.T, h http.HandlerFunc, opts VolumeHealthOptions) volumehealth.SourceReader {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	if opts.Namespace == "" {
		opts.Namespace = "team-a"
	}
	reader, err := NewVolumeHealthSource(&rest.Config{Host: server.URL, BearerToken: "caller"}, opts)
	if err != nil {
		t.Fatal(err)
	}
	return reader
}

func TestHealthSourcesRemainIndependentAndPrivate(t *testing.T) {
	f := newHealthFixture()
	f.pod.Status.VolumeHealth = []corev1.PodVolumeHealth{{Name: "data", HealthConditions: []corev1.VolumeHealthCondition{{Status: "FutureDiskState", Reason: "NodeReason", Message: "private-node-message"}}}}
	f.pvc.Status.HealthStatus = &corev1.VolumeHealthStatus{}
	f.node.Status.StorageHealth = []storagev1.StorageHealth{{Name: "unrelated-driver", HealthConditions: []storagev1.StorageHealthCondition{{Status: "StorageUnreachable", Message: "other-tenant"}}}, {Name: "storage.example.test", HealthConditions: []storagev1.StorageHealthCondition{{Status: "StorageDegraded", Reason: "BackendReason", Message: "private-backend-message"}}}}
	r, err := testHealthReader(t, f.handler(t, ""), VolumeHealthOptions{}).Query(context.Background(), "workload")
	if err != nil {
		t.Fatal(err)
	}
	rows := r.Interactive()
	if len(rows) != 3 || rows[0].State != volumehealth.StateAdverse || !rows[0].UnknownStatus || rows[1].State != volumehealth.StateHealthy || rows[2].State != volumehealth.StateAdverse {
		t.Fatalf("source separation lost: %v", r)
	}
	if rows[0].Conditions[0].Status != "FutureDiskState" || rows[1].Identity.PVCUID != "claim-a" || rows[2].Identity.Driver != "storage.example.test" {
		t.Fatal("authorised identity/status lost")
	}
	encoded, _ := json.Marshal(r)
	for _, secret := range []string{"workload", "data", "team-a", "private", "other-tenant", "FutureDiskState", "NodeReason"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("export leaked %s", secret)
		}
	}
}

func TestOldAndMixedKubeletHealthIsUnreported(t *testing.T) {
	for _, backendReported := range []bool{false, true} {
		f := newHealthFixture()
		if backendReported {
			f.node.Status.StorageHealth = []storagev1.StorageHealth{{Name: "storage.example.test", HealthConditions: []storagev1.StorageHealthCondition{{Status: "StorageDegraded"}}}}
		}
		r, err := testHealthReader(t, f.handler(t, ""), VolumeHealthOptions{}).Query(context.Background(), "workload")
		if err != nil {
			t.Fatal(err)
		}
		rows := r.Interactive()
		if rows[0].Availability != volumehealth.Unreported || rows[1].Availability != volumehealth.Unreported || rows[0].State == volumehealth.StateHealthy {
			t.Fatal("old/missing kubelet fields became healthy")
		}
		if rows[2].Adverse != backendReported {
			t.Fatal("backend evidence was copied to another source")
		}
	}
}

func TestObjectPermissionsDoNotFallBack(t *testing.T) {
	for _, denied := range []string{healthPodPath, healthClaimPath, healthPVPath, healthNodePath} {
		t.Run(denied, func(t *testing.T) {
			f := newHealthFixture()
			f.pvc.Status.HealthStatus = &corev1.VolumeHealthStatus{}
			r, err := testHealthReader(t, f.handler(t, denied), VolumeHealthOptions{}).Query(context.Background(), "workload")
			if denied == healthPodPath {
				if err == nil || r.Summary().Observations != 0 || strings.Contains(err.Error(), "private") {
					t.Fatalf("root denial: %v %v", r, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			rows := r.Interactive()
			index := 2
			if denied == healthClaimPath {
				index = 1
				if rows[1].Identity.PVCName != "" {
					t.Fatal("denied claim identity exposed")
				}
			}
			if rows[index].Availability != volumehealth.Forbidden {
				t.Fatalf("denial not retained: %v", r)
			}
		})
	}
}
