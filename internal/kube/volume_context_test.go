package kube

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
)

type volumeResolverFixture struct {
	pod            corev1.Pod
	pvc            corev1.PersistentVolumeClaim
	pv             corev1.PersistentVolume
	csinode        storagev1.CSINode
	backendStatus  int
	config         *rest.Config
	denied         map[string]bool
	reads          []string
	authorisations []VolumeAccess
	missing        bool
	replaceAtFinal bool
	podReads       int
	resolver       VolumeResolver
}

func newVolumeResolverFixture(t *testing.T) *volumeResolverFixture {
	t.Helper()
	now := metav1.NewTime(time.Now().UTC().Add(-time.Hour))
	f := &volumeResolverFixture{denied: map[string]bool{}}
	f.pod = corev1.Pod{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"}, ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: "app", UID: "pod-uid", CreationTimestamp: now}, Spec: corev1.PodSpec{NodeName: "node-a", Volumes: []corev1.Volume{{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "claim"}}}}, Containers: []corev1.Container{{Name: "app", VolumeMounts: []corev1.VolumeMount{{Name: "data", MountPath: "/private/mount"}}}}}}
	f.pvc = corev1.PersistentVolumeClaim{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PersistentVolumeClaim"}, ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: "claim", UID: "claim-uid", CreationTimestamp: now}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "bound-pv"}}
	f.pv = corev1.PersistentVolume{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PersistentVolume"}, ObjectMeta: metav1.ObjectMeta{Name: "bound-pv", UID: "pv-uid"}, Spec: corev1.PersistentVolumeSpec{ClaimRef: &corev1.ObjectReference{Namespace: "tenant-a", Name: "claim", UID: "claim-uid"}, PersistentVolumeSource: corev1.PersistentVolumeSource{CSI: &corev1.CSIPersistentVolumeSource{Driver: "fixture.csi.test", VolumeHandle: "private-backend-handle"}}}}
	f.csinode = storagev1.CSINode{TypeMeta: metav1.TypeMeta{APIVersion: "storage.k8s.io/v1", Kind: "CSINode"}, ObjectMeta: metav1.ObjectMeta{Name: "node-a", UID: "csi-uid"}, Spec: storagev1.CSINodeSpec{Drivers: []storagev1.CSINodeDriver{{Name: "fixture.csi.test", NodeID: "private-node-id"}}}}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.reads = append(f.reads, r.URL.Path)
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer collector-acquisition" || r.Header.Get("Impersonate-User") != "" {
			t.Error("unexpected acquisition identity or verb")
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/namespaces/tenant-a/pods/"):
			f.podReads++
			if f.missing {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			pod := f.pod
			if f.replaceAtFinal && f.podReads%2 == 0 {
				pod.UID = "replaced-pod"
			}
			_ = json.NewEncoder(w).Encode(pod)
		case r.URL.Path == "/api/v1/namespaces/tenant-a/persistentvolumeclaims/"+f.pvc.Name:
			_ = json.NewEncoder(w).Encode(f.pvc)
		case r.URL.Path == "/api/v1/persistentvolumes/bound-pv":
			_ = json.NewEncoder(w).Encode(f.pv)
		case r.URL.Path == "/apis/storage.k8s.io/v1/csinodes/node-a":
			if f.backendStatus != 0 {
				w.WriteHeader(f.backendStatus)
				return
			}
			_ = json.NewEncoder(w).Encode(f.csinode)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	config := &rest.Config{Host: server.URL, BearerToken: "collector-acquisition", TLSClientConfig: rest.TLSClientConfig{CAData: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})}}
	f.config = config
	resolver, err := NewVolumeResolver(config, func(_ context.Context, a VolumeAccess) error {
		f.authorisations = append(f.authorisations, a)
		if f.denied[a.Resource] {
			return &HealthReadError{Reason: volumehealth.AccessDenied}
		}
		return nil
	}, func(name string, _ time.Time) (string, bool) { return "node-uid", name == "node-a" })
	if err != nil {
		t.Fatal(err)
	}
	f.resolver = resolver
	return f
}

func TestVolumeResolverPreservesNamespacePVCViewerWithoutPVDisclosure(t *testing.T) {
	f := newVolumeResolverFixture(t)
	f.denied["persistentvolumes"] = true
	r, err := f.resolver.Resolve(t.Context(), "tenant-a", "app")
	if err != nil {
		t.Fatal(err)
	}
	if r.Scope.PodUID != "pod-uid" || len(r.Bindings) != 1 || r.Bindings[0].PVCUID != "claim-uid" || r.Bindings[0].Driver != "" || r.Bindings[0].Configuration.MountCount != 1 {
		t.Fatal("namespace binding or disclosure changed")
	}
	if len(f.reads) != 4 {
		t.Fatal("binding reads are not bounded to the requested objects")
	}
	for _, a := range f.authorisations {
		if a.Resource == "nodes" {
			t.Fatal("namespace reader unexpectedly needs Node access")
		}
	}
	f.denied["persistentvolumes"] = false
	r, err = f.resolver.Resolve(t.Context(), "tenant-a", "app")
	if err != nil || r.Bindings[0].Driver != "fixture.csi.test" {
		t.Fatal("authorised driver unavailable", err)
	}
}

func TestVolumeResolverChecksEveryCallerAndClearsDeniedClaim(t *testing.T) {
	f := newVolumeResolverFixture(t)
	if _, err := f.resolver.Resolve(t.Context(), "tenant-a", "app"); err != nil {
		t.Fatal(err)
	}
	f.denied["persistentvolumeclaims"] = true
	r, err := f.resolver.Resolve(t.Context(), "tenant-a", "app")
	if err != nil {
		t.Fatal(err)
	}
	b := r.Bindings[0]
	if b.PVCName != "" || b.PVCUID != "" || b.Driver != "" || b.ClaimAvailability != volumehealth.Forbidden {
		t.Fatal("denied claim retained previous identity")
	}
	f.denied["pods"] = true
	before := len(f.reads)
	for _, name := range []string{"app", "missing"} {
		_, err := f.resolver.Resolve(t.Context(), "tenant-a", name)
		var failure *HealthReadError
		if !errors.As(err, &failure) || failure.Reason != volumehealth.AccessDenied {
			t.Fatal("denied target disclosed existence", err)
		}
	}
	if len(f.reads) != before {
		t.Fatal("Pod denial still read privileged objects")
	}
}

func TestVolumeResolverRejectsCrossNamespaceAndReplacedBindings(t *testing.T) {
	for _, mutate := range []func(*volumeResolverFixture){
		func(f *volumeResolverFixture) { f.pvc.Namespace = "tenant-b" },
		func(f *volumeResolverFixture) { f.pod.Namespace = "tenant-b" },
		func(f *volumeResolverFixture) { f.replaceAtFinal = true },
	} {
		f := newVolumeResolverFixture(t)
		mutate(f)
		if _, err := f.resolver.Resolve(t.Context(), "tenant-a", "app"); err == nil {
			t.Fatal("cross-scope or replaced Pod accepted")
		}
	}
	f := newVolumeResolverFixture(t)
	f.pv.Spec.ClaimRef.UID = "another-claim"
	r, err := f.resolver.Resolve(t.Context(), "tenant-a", "app")
	if err != nil {
		t.Fatal(err)
	}
	if r.Bindings[0].PVCUID != "" || r.Bindings[0].ClaimAvailability != volumehealth.Unavailable {
		t.Fatal("PV claim mismatch became a valid binding")
	}
	f.missing = true
	if _, err := f.resolver.Resolve(t.Context(), "tenant-a", "app"); !errors.Is(err, ErrVolumePodNotFound) {
		t.Fatal("deleted Pod was not explicit", err)
	}
}

func TestVolumeResolverVerifiesGenericEphemeralOwner(t *testing.T) {
	f := newVolumeResolverFixture(t)
	f.pod.Spec.Volumes[0].PersistentVolumeClaim = nil
	f.pod.Spec.Volumes[0].Ephemeral = &corev1.EphemeralVolumeSource{}
	f.pvc.Name = "app-data"
	f.pv.Spec.ClaimRef.Name = "app-data"
	r, err := f.resolver.Resolve(t.Context(), "tenant-a", "app")
	if err != nil {
		t.Fatal(err)
	}
	if r.Bindings[0].PVCUID != "" {
		t.Fatal("unowned ephemeral claim joined")
	}
	controller := true
	f.pvc.OwnerReferences = []metav1.OwnerReference{{APIVersion: "v1", Kind: "Pod", Name: "app", UID: types.UID("pod-uid"), Controller: &controller}}
	r, err = f.resolver.Resolve(t.Context(), "tenant-a", "app")
	if err != nil || r.Bindings[0].PVCUID != "claim-uid" {
		t.Fatal("owned ephemeral claim did not join", err)
	}
}
