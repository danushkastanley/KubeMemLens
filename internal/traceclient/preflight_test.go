package traceclient

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPreflightRejectsChangedBoundsPolicyVersionAndQualification(t *testing.T) {
	for name, change := range map[string]func(*admission.PreflightDocument){
		"bounds":           func(d *admission.PreflightDocument) { d.Bounds.Events-- },
		"paths":            func(d *admission.PreflightDocument) { d.Paths = trace.ConfirmedPaths },
		"kind":             func(d *admission.PreflightDocument) { d.TraceKind = trace.Cache },
		"request-version":  func(d *admission.PreflightDocument) { d.RequestSchemaVersion = 1 },
		"response-version": func(d *admission.PreflightDocument) { d.SchemaVersion = 2 },
		"qualification":    func(d *admission.PreflightDocument) { d.ResourceQualified = true },
		"programme":        func(d *admission.PreflightDocument) { d.Node.ProgrammeDigest = "unverified" },
	} {
		t.Run(name, func(t *testing.T) {
			doc := preflightFixture(t, selectionFixture(), DefaultIntent(trace.Files))
			change(&doc)
			c, _ := fixtureClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == apiPrefix {
					discoverFixture(w)
					return
				}
				_ = json.NewEncoder(w).Encode(doc)
			}))
			if _, err := c.Preflight(context.Background(), selectionFixture(), DefaultIntent(trace.Files)); err == nil {
				t.Fatal("changed preflight accepted")
			}
		})
	}
}

func TestAbsentExtensionNeverFallsBackToAdmission(t *testing.T) {
	var posts atomic.Int64
	c, _ := fixtureClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts.Add(1)
		}
		http.NotFound(w, r)
	}))
	if _, err := c.Preflight(context.Background(), selectionFixture(), DefaultIntent(trace.Files)); err == nil || posts.Load() != 0 {
		t.Fatal("absent extension reached mutation")
	}
}

func TestSelectReadsOneExactRunningContainerLifetime(t *testing.T) {
	s := selectionFixture()
	for _, scenario := range []string{"valid", "other-pod", "deleting", "duplicate", "stopped", "short-id"} {
		t.Run(scenario, func(t *testing.T) {
			pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: s.Pod, Namespace: s.Namespace, UID: "selected-uid"}, Spec: corev1.PodSpec{NodeName: s.NodeName}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: s.Container, ContainerID: "containerd://" + s.ContainerID, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(s.ContainerStartedAt)}}}}}}
			switch scenario {
			case "other-pod":
				pod.Name = "replacement"
			case "deleting":
				now := metav1.NewTime(time.Now())
				pod.DeletionTimestamp = &now
			case "duplicate":
				pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, pod.Status.ContainerStatuses[0])
			case "stopped":
				pod.Status.ContainerStatuses[0].State.Running = nil
			case "short-id":
				pod.Status.ContainerStatuses[0].ContainerID = "containerd://short"
			}
			c, _ := fixtureClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/namespaces/tenant-a/pods/selected-pod" || r.URL.RawQuery != "" {
					t.Error("selection read changed scope")
				}
				_ = json.NewEncoder(w).Encode(pod)
			}))
			selected, err := c.Select(context.Background(), s.Namespace, s.Pod, s.Container)
			if scenario == "valid" {
				if err != nil || selected != s {
					t.Fatal("selected lifetime changed", err)
				}
				pin := SelectionPin{PodUID: s.PodUID, ContainerID: "containerd://" + s.ContainerID, NodeName: s.NodeName}
				if pinned, err := c.SelectPinned(context.Background(), s.Namespace, s.Pod, s.Container, pin); err != nil || pinned != s {
					t.Fatal("matching displayed selection rejected", err)
				}
				for _, change := range []func(*SelectionPin){func(p *SelectionPin) { p.PodUID = "old-uid" }, func(p *SelectionPin) { p.ContainerID = strings.Repeat("b", 64) }, func(p *SelectionPin) { p.NodeName = "old-node" }} {
					old := pin
					change(&old)
					if _, err := c.SelectPinned(context.Background(), s.Namespace, s.Pod, s.Container, old); err == nil {
						t.Fatal("displayed target silently replaced")
					}
				}
			} else if err == nil {
				t.Fatal("invalid target selected")
			}
		})
	}
}
