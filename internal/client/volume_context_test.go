package client

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

func TestPodVolumesValidatesInstanceAndDoesNotFallback(t *testing.T) {
	response := api.PodVolumeContext{TypeMeta: metav1.TypeMeta{APIVersion: "memory.kubememlens.io/v1alpha1", Kind: "PodVolumeContext"}, ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-a", Name: "app", UID: "pod-uid"}, Context: volumecontext.View{SchemaVersion: volumecontext.SchemaVersion, Namespace: "tenant-a", PodName: "app"}}
	response.Context.Volumes = []volumecontext.NamedVolume{{VolumeName: "data", Configuration: volumecontext.Configuration{Kind: volumecontext.EmptyDir}, Usage: volumecontext.SourceState(volumehealth.Unreported, volumecontext.NoReport)}}
	status, calls := http.StatusOK, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/apis/memory.kubememlens.io/v1alpha1/namespaces/tenant-a/pods/app/volumes" || r.Method != http.MethodGet {
			t.Error("unexpected volume read path")
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()
	scope, err := NamespaceScope("tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := NewKubernetesAPIClient(&rest.Config{Host: server.URL}, scope, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.PodVolumes(t.Context(), "tenant-a", "app", "pod-uid"); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.PodVolumes(t.Context(), "tenant-a", "app", "old-uid"); err == nil {
		t.Fatal("replaced selection accepted")
	}
	response.Context.Namespace = "tenant-b"
	if _, err := reader.PodVolumes(t.Context(), "tenant-a", "app", ""); err == nil {
		t.Fatal("cross-namespace response accepted")
	}
	status = http.StatusForbidden
	_, err = reader.PodVolumes(t.Context(), "tenant-a", "app", "")
	var failure *ReadError
	if !errors.As(err, &failure) || failure.Kind != ReadErrorForbidden || calls != 4 {
		t.Fatal("denial triggered fallback", err)
	}
	if _, err := reader.PodVolumes(t.Context(), "../tenant-a", "app", ""); err == nil || calls != 4 {
		t.Fatal("invalid name reached transport")
	}
	if _, err := reader.PodVolumes(t.Context(), "tenant-b", "app", ""); err == nil || calls != 4 {
		t.Fatal("configured namespace scope was widened")
	}
}

func TestPodVolumesRejectsBoundedHostileResponse(t *testing.T) {
	for _, body := range []string{
		`{"context":{"volumes":[` + strings.Repeat(`{},`, volumecontext.MaxVolumesPerPod) + `{}]}}`,
		`{"context":{"schemaVersion":1,"SchemaVersion":1}}`,
		strings.Repeat(" ", volumecontext.MaxPageBytes+1),
	} {
		for _, chunked := range []bool{false, true} {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if chunked {
					w.(http.Flusher).Flush()
				}
				_, _ = w.Write([]byte(body))
			}))
			reader, err := NewKubernetesAPIClient(&rest.Config{Host: server.URL}, AllNamespacesScope(), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := reader.PodVolumes(t.Context(), "tenant-a", "app", ""); err == nil {
				t.Fatal("hostile response accepted")
			}
			server.Close()
		}
	}
}
