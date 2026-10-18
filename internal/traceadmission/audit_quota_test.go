package traceadmission

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
)

func TestEveryQuotaScopeRemainsBoundedAndAuditedUnderConcurrency(t *testing.T) {
	for _, scope := range []string{"actor", "tenant", "node", "cluster"} {
		t.Run(scope, func(t *testing.T) {
			policy := DefaultPolicy()
			policy.PerPrincipal, policy.PerNamespace, policy.PerNode, policy.Global = 2, 4, 2, 64
			switch scope {
			case "actor":
				policy.PerPrincipal = 1
			case "tenant":
				policy.PerNamespace = 1
			case "node":
				policy.PerNode = 1
			case "cluster":
				policy.Global = 1
			}
			h := newHarness(t, policy)
			h.resolver.resolve = func(_ context.Context, r Request) (Workload, error) {
				workload := testWorkload(r)
				workload.Target.PodUID = "uid-" + r.pod
				id := sha256.Sum256([]byte(r.namespace + "/" + r.pod))
				workload.Target.ContainerID = hex.EncodeToString(id[:])
				if scope != "node" {
					workload.NodeName = "node-" + r.pod
					workload.Target.NodeUID = workload.NodeName
				}
				return workload, nil
			}
			const requests = 16
			results := make(chan error, requests)
			start := make(chan struct{})
			for index := range requests {
				namespace, name := fmt.Sprintf("tenant-%d", index), fmt.Sprintf("actor-%d", index)
				if scope == "actor" {
					name = "same-actor"
				}
				if scope == "tenant" {
					namespace = "same-tenant"
				}
				request := requestFor(t, namespace)
				request.pod = fmt.Sprintf("pod-%d", index)
				go func() { <-start; _, err := h.manager.Admit(context.Background(), actor(name), request); results <- err }()
			}
			close(start)
			accepted := 0
			for range requests {
				err := <-results
				if err == nil {
					accepted++
				} else if !errors.Is(err, ErrCapacity) {
					t.Fatal("unexpected rejection", err)
				}
			}
			if accepted != 1 || h.binder.calls.Load() != 1 {
				t.Fatal("quota widened under contention", accepted, h.binder.calls.Load())
			}
			h.mu.Lock()
			records := append([]AuditEvent(nil), h.audits...)
			h.mu.Unlock()
			denied := 0
			for _, record := range records {
				event := auditFields(t, record)
				if event["operation"] == "terminal" {
					t.Fatal("rejected reservation invented engine termination")
				}
				if event["operation"] != "create" || event["decision"] != "rejected" {
					continue
				}
				if event["reason"] != "capacity" || event["actorRef"] == nil || event["tenantRef"] == nil || event["targetRef"] != nil {
					t.Fatal("quota rejection leaked or lost context")
				}
				denied++
			}
			if denied != requests-1 {
				t.Fatal("missing quota decisions", denied)
			}
		})
	}
}
