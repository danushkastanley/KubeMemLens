package client

import (
	"context"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type volumeEvidenceReader struct {
	pod         api.PodSnapshot
	volumes     api.PodVolumeContext
	volumeErr   error
	calls       int
	expectedUID string
}

func (r *volumeEvidenceReader) Pod(context.Context, string, string) (api.PodSnapshot, error) {
	return r.pod, nil
}
func (r *volumeEvidenceReader) PodVolumes(_ context.Context, _, _, uid string) (api.PodVolumeContext, error) {
	r.calls++
	r.expectedUID = uid
	return r.volumes, r.volumeErr
}

func TestPodVolumeEvidenceBindsInstanceAndClearsOnDenial(t *testing.T) {
	for _, test := range []struct {
		name     string
		change   func(*volumeEvidenceReader)
		expected string
		ok       bool
	}{
		{"current", func(*volumeEvidenceReader) {}, "uid", true},
		{"replaced selection", func(*volumeEvidenceReader) {}, "old-uid", false},
		{"replaced volume Pod", func(r *volumeEvidenceReader) { r.volumes.UID = "new-uid" }, "uid", false},
		{"volume tenant", func(r *volumeEvidenceReader) { r.volumes.Context.Namespace = "other" }, "uid", false},
		{"memory tenant", func(r *volumeEvidenceReader) { r.pod.Namespace = "other" }, "uid", false},
		{"revoked", func(r *volumeEvidenceReader) { r.volumeErr = &ReadError{Kind: ReadErrorForbidden} }, "uid", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := &volumeEvidenceReader{pod: api.PodSnapshot{Namespace: "team", PodName: "app", PodUID: "uid"}, volumes: api.PodVolumeContext{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "app", UID: "uid"}, Context: volumecontext.View{SchemaVersion: 1, Namespace: "team", PodName: "app"}}}
			test.change(r)
			pod, volumes, err := ReadPodVolumeEvidence(t.Context(), r, "team", "app", test.expected)
			if (err == nil) != test.ok {
				t.Fatalf("unexpected outcome: %v", err)
			}
			if !test.ok && (pod.PodUID != "" || volumes.UID != "") {
				t.Fatal("partial protected evidence returned after failure")
			}
			if r.calls > 0 && r.expectedUID != "uid" {
				t.Fatal("volume read omitted expected instance")
			}
		})
	}
}
