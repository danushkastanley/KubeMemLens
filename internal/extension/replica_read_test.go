package extension

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/changemarkers"
	"github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"github.com/danushkastanley/kube-memlens/internal/replicabaseline"
)

type replicaResolveFunc func(context.Context, memoryhistory.Request) (replicabaseline.Selection, error)

func (f replicaResolveFunc) Resolve(ctx context.Context, r memoryhistory.Request) (replicabaseline.Selection, error) {
	return f(ctx, r)
}

const replicaPath = "/apis/memory.kubememlens.io/v1alpha1/namespaces/team-a/pods/api-0/replicas"

func replicaReadFixture(t *testing.T) (*ReadHandler, replicabaseline.Selection) {
	t.Helper()
	h, now := populatedReadHandler(t)
	root := changemarkers.Object{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "team-a", Name: "api", UID: "workload-uid"}
	s := replicabaseline.Selection{Request: memoryhistory.Request{Scope: memoryhistory.Pod, Namespace: "team-a", Name: "api-0"}, Workload: root, ResolvedAt: now, EventState: changemarkers.Missing}
	for i := 0; i < 5; i++ {
		pod := changemarkers.Object{APIVersion: "v1", Kind: "Pod", Namespace: "team-a", Name: fmt.Sprintf("api-%d", i), UID: fmt.Sprintf("pod-%d", i)}
		s.Members = append(s.Members, replicabaseline.Member{Object: pod, Owners: []changemarkers.Object{root}, Revision: "rs-uid", Shape: strings.Repeat("a", 64), Lifecycle: replicabaseline.Ready, Changes: replicabaseline.ChangeUnknown, StableSince: now.Add(-time.Hour)})
	}
	s.Requested = s.Members[0].Object
	h.replicas = &replicaService{namespaces: map[string]bool{"team-a": true}, gate: make(chan struct{}, 1), resolver: replicaResolveFunc(func(context.Context, memoryhistory.Request) (replicabaseline.Selection, error) { return s, nil }), evidence: func(_ context.Context, s replicabaseline.Selection, now time.Time) (replicabaseline.Input, error) {
		input := replicabaseline.Input{Workload: s.Workload, ObservedAt: now}
		for i, m := range s.Members {
			value := float64(32 << 20)
			if i == 0 {
				value = 96 << 20
			}
			input.Peers = append(input.Peers, replicabaseline.Peer{Object: m.Object, WorkloadUID: s.Workload.UID, Revision: m.Revision, Shape: m.Shape, Lifecycle: m.Lifecycle, Changes: m.Changes, StableSince: m.StableSince, CapturedAt: now, SampleState: replicabaseline.Available, HistoryState: replicabaseline.Unreported, Values: map[replicabaseline.Metric]replicabaseline.Value{replicabaseline.Charge: {State: replicabaseline.Available, Number: value}}})
		}
		return input, nil
	}}
	return h, s
}

func TestReplicaAPIRevalidatesAndReturnsBoundedInformationalEvidence(t *testing.T) {
	h, s := replicaReadFixture(t)
	calls := 0
	h.replicas.resolver = replicaResolveFunc(func(context.Context, memoryhistory.Request) (replicabaseline.Selection, error) {
		calls++
		s.ResolvedAt = s.ResolvedAt.Add(time.Millisecond)
		return s, nil
	})
	response := serveRead(t, h, replicaPath)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var r replicaResource
	if err := json.Unmarshal(response.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || r.Requested != s.Requested || r.Baseline.Validate() != nil || r.Baseline.Peers[0].Comparisons[0].Outlier != "higher" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("invalid API evidence")
	}
	h.replicas = nil
	if got := serveRead(t, h, replicaPath).Code; got != 404 {
		t.Fatal("disabled replica API visible", got)
	}
}

func TestReplicaAPIDiscardsEvidenceAfterRevocationOrMembershipChange(t *testing.T) {
	for _, change := range []string{"denied initially", "revoked", "UID", "revision", "shape", "node", "events", "member count"} {
		t.Run(change, func(t *testing.T) {
			h, s := replicaReadFixture(t)
			calls, reads := 0, 0
			prior := h.replicas.evidence
			h.replicas.evidence = func(ctx context.Context, s replicabaseline.Selection, now time.Time) (replicabaseline.Input, error) {
				reads++
				return prior(ctx, s, now)
			}
			h.replicas.resolver = replicaResolveFunc(func(context.Context, memoryhistory.Request) (replicabaseline.Selection, error) {
				calls++
				if change == "denied initially" || change == "revoked" && calls == 2 {
					return replicabaseline.Selection{}, memoryhistory.ErrDenied
				}
				result := s
				result.Members = append([]replicabaseline.Member(nil), s.Members...)
				if calls == 2 {
					switch change {
					case "UID":
						result.Members[0].Object.UID = "replacement"
					case "revision":
						result.Members[0].Revision = "new-revision"
					case "shape":
						result.Members[0].Shape = strings.Repeat("b", 64)
					case "node":
						result.Members[0].NodeUID = "replacement-node"
					case "events":
						result.EventState = changemarkers.Denied
					case "member count":
						result.Members = result.Members[:4]
					}
				}
				return result, nil
			})
			response := serveRead(t, h, replicaPath)
			want := http.StatusConflict
			if change == "denied initially" || change == "revoked" {
				want = http.StatusForbidden
			}
			if response.Code != want || strings.Contains(response.Body.String(), "pod-0") || strings.Contains(response.Body.String(), "96") {
				t.Fatal("changed or denied evidence disclosed", response.Code, response.Body.String())
			}
			if change == "denied initially" && reads != 0 {
				t.Fatal("denied request read store")
			}
		})
	}
}

func TestReplicaAPIRejectsUnselectedEvidenceAndInvalidQueries(t *testing.T) {
	for _, path := range []string{strings.Replace(replicaPath, "team-a", "team-b", 1), replicaPath + "?selector=all", replicaPath + "?kind=Deployment", strings.Replace(replicaPath, "pods/api-0", "workloads/api", 1) + "?kind=Deployment&kind=Job"} {
		h, _ := replicaReadFixture(t)
		h.replicas.resolver = replicaResolveFunc(func(context.Context, memoryhistory.Request) (replicabaseline.Selection, error) {
			t.Error("invalid query acquired metadata")
			return replicabaseline.Selection{}, nil
		})
		if response := serveRead(t, h, path); response.Code == 200 {
			t.Fatal("invalid scope accepted")
		}
	}
	h, _ := replicaReadFixture(t)
	original := h.replicas.evidence
	h.replicas.evidence = func(ctx context.Context, s replicabaseline.Selection, now time.Time) (replicabaseline.Input, error) {
		input, err := original(ctx, s, now)
		input.Peers[0].Object.Name = "hidden"
		return input, err
	}
	if response := serveRead(t, h, replicaPath); response.Code == 200 || strings.Contains(response.Body.String(), "hidden") {
		t.Fatal("foreign evidence disclosed")
	}
}
