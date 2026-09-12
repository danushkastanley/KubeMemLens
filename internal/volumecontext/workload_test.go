package volumecontext

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
)

func sharedClaimViews(t *testing.T) []View {
	t.Helper()
	var views []View
	for i, name := range []string{"pod-a", "pod-b"} {
		s, b, u := fixture()
		s.PodName = name
		s.PodUID = name + "-uid"
		b[0].PVUID = "private-pv-uid"
		u[0].PodUID = s.PodUID
		u[0].Filesystem.CapturedAt = testNow.Add(time.Duration(i) * time.Second)
		r, err := Join(s, b, u, nil, SourceState(volumehealth.Reported, ""), testNow.Add(time.Second))
		if err != nil {
			t.Fatal(err)
		}
		view := r.Authorised()
		views = append(views, view)
		redacted, _ := json.Marshal(r)
		if strings.Contains(string(redacted), "evidenceID") || strings.Contains(string(redacted), "filesystemID") || strings.Contains(string(redacted), "private-pv") {
			t.Fatal("redacted report exposed binding references")
		}
	}
	return views
}

func TestWorkloadDeduplicatesImmutableClaimBindings(t *testing.T) {
	views := sharedClaimViews(t)
	if views[0].Volumes[0].EvidenceID == views[1].Volumes[0].EvidenceID || views[0].Volumes[0].FilesystemID != views[1].Volumes[0].FilesystemID {
		t.Fatal("Pod and filesystem identities conflated")
	}
	groups, err := GroupWorkload(views, testNow.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || len(groups[0].Members) != 2 || groups[0].Selected.Pod != 1 {
		t.Fatal("shared claim counted once per replica")
	}
	views[1].Volumes[0].FilesystemID = identityDigest("replacement-pv")
	groups, err = GroupWorkload(views, testNow.Add(time.Second))
	if err != nil || len(groups) != 2 {
		t.Fatal("replacement filesystem merged")
	}
	for i := range views {
		views[i].Volumes[0].EvidenceID = ""
		views[i].Volumes[0].FilesystemID = ""
	}
	groups, err = GroupWorkload(views, testNow.Add(time.Second))
	if err != nil || len(groups) != 2 {
		t.Fatal("names substituted for missing binding identity")
	}
}

func TestWorkloadIdentityAndCountBounds(t *testing.T) {
	views := sharedClaimViews(t)
	views[1].Namespace = "other"
	if _, err := GroupWorkload(views, testNow.Add(time.Second)); err == nil {
		t.Fatal("cross-namespace composition accepted")
	}
	views = sharedClaimViews(t)
	views[0].Volumes[0].EvidenceID = "not-a-binding-reference"
	if ValidateView(views[0], testNow.Add(time.Second)) == nil {
		t.Fatal("invalid reference accepted")
	}
	if _, err := GroupWorkload(make([]View, MaxWorkloadPods+1), testNow); err == nil {
		t.Fatal("oversized workload accepted")
	}
}
