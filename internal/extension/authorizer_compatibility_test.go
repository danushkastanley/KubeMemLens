package extension

import (
	"context"
	"errors"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
)

func TestConditionsAwareAuthorizationPreservesDelegation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		decision authorizer.Decision
		err      error
		want     authorizer.Decision
	}{
		{"allow", authorizer.DecisionAllow, nil, authorizer.DecisionAllow},
		{"deny", authorizer.DecisionDeny, nil, authorizer.DecisionDeny},
		{"no opinion", authorizer.DecisionNoOpinion, nil, authorizer.DecisionNoOpinion},
		{"error overrides allow", authorizer.DecisionAllow, errors.New("private upstream detail"), authorizer.DecisionNoOpinion},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, records := 0, 0
			attributes := &authorizer.AttributesRecord{
				APIGroup: api.MemoryAPIGroup, Resource: "pods", Verb: "list", Namespace: "tenant-a",
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			gate := agentIdentityAuthorizer{
				delegate: authorizer.AuthorizerFunc(func(gotContext context.Context, gotAttributes authorizer.Attributes) (authorizer.Decision, string, error) {
					calls++
					if gotContext != ctx || gotAttributes != attributes {
						t.Error("request context or scope changed before delegation")
					}
					return tc.decision, "delegated", tc.err
				}),
				logf: func(string, ...any) { records++ },
			}
			result := gate.ConditionsAwareAuthorize(ctx, attributes)
			if !result.IsUnconditional() || !result.PossibleDecisions().Equal(authorizer.ConditionsAwareDecisionFromParts(tc.want, "", nil).PossibleDecisions()) {
				t.Fatalf("decision changed or became conditional: %+v", result)
			}
			if calls != 1 || records != 1 {
				t.Fatalf("delegation=%d audit=%d, want one of each", calls, records)
			}
			if tc.err != nil {
				if !errors.Is(result.Error(), errDelegatedAuthorisation) || result.Reason() != errDelegatedAuthorisation.Error() {
					t.Fatalf("delegated error was not sanitised: %+v", result)
				}
				return
			}
			if result.Error() != nil || result.Reason() != "delegated" {
				t.Fatalf("delegated result changed: %+v", result)
			}
		})
	}
}

func TestConditionsAwareAuthorizationRejectsInvalidAgentBeforeDelegation(t *testing.T) {
	delegated := 0
	gate := agentIdentityAuthorizer{
		expectedUsername: "system:serviceaccount:kube-memlens:kube-memlens-agent",
		delegate: authorizer.AuthorizerFunc(func(context.Context, authorizer.Attributes) (authorizer.Decision, string, error) {
			delegated++
			return authorizer.DecisionAllow, "", nil
		}),
	}
	for _, resource := range []string{"nodesnapshots", "ingestionepochs"} {
		result := gate.ConditionsAwareAuthorize(context.Background(), authorizer.AttributesRecord{
			APIGroup: api.MemoryAPIGroup, Resource: resource, Verb: "create", ResourceRequest: true,
			User: &user.DefaultInfo{Name: "system:serviceaccount:kube-memlens:other"},
		})
		if !result.IsDeny() || result.Error() != nil || result.Reason() != "agent identity is invalid" {
			t.Fatalf("%s accepted invalid identity: %+v", resource, result)
		}
	}
	if delegated != 0 {
		t.Fatal("invalid agent identity reached the delegate")
	}
	result := gate.ConditionsAwareAuthorize(context.Background(), authorizer.AttributesRecord{
		APIGroup: api.MemoryAPIGroup, Resource: "nodesnapshots", Verb: "create", ResourceRequest: true,
		User: &user.DefaultInfo{
			Name: gate.expectedUsername,
			Extra: map[string][]string{
				PodUIDExtra: {"pod-a"}, NodeNameExtra: {"node-a"}, NodeUIDExtra: {"node-uid-a"}, CredentialIDExtra: {"credential-a"},
			},
		},
	})
	if !result.IsAllow() || result.Error() != nil || delegated != 1 {
		t.Fatalf("valid agent did not delegate once: %+v calls=%d", result, delegated)
	}
}

func TestUnsupportedConditionEvaluationFailsClosed(t *testing.T) {
	gate := agentIdentityAuthorizer{}
	decision, reason, err := gate.EvaluateConditions(context.Background(), authorizer.ConditionsAwareDecisionAllow("", nil), nil)
	if decision != authorizer.DecisionDeny || reason != "" || !errors.Is(err, authorizer.ErrorConditionEvaluationNotSupported) {
		t.Fatalf("unsupported conditions did not fail closed: %v %q %v", decision, reason, err)
	}
}
