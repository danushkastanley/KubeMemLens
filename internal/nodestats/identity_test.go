package nodestats

import (
	"net/http"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
	corev1 "k8s.io/api/core/v1"
)

func TestPodBoundIdentityIsRequired(t *testing.T) {
	for _, test := range []struct {
		name, body string
		reason     nodecontext.Reason
	}{
		{"other-node", strings.Replace(identityBody(), `"node-a"`, `"node-b"`, 1), nodecontext.InvalidTarget},
		{"unbound-user", strings.Replace(identityBody(), `"system:serviceaccount:fixture:node-context"`, `"admin"`, 1), nodecontext.Authentication},
		{"missing-pod", strings.Replace(identityBody(), `["pod-uid-a"]`, `[]`, 1), nodecontext.Authentication},
		{"multiple-nodes", strings.Replace(identityBody(), `["node-a"]`, `["node-a","node-b"]`, 1), nodecontext.Authentication},
		{"missing-node-uid", strings.Replace(identityBody(), `["node-uid-a"]`, `[]`, 1), nodecontext.Authentication},
		{"duplicate-extra", strings.Replace(identityBody(), `"authentication.kubernetes.io/node-name":["node-a"]`, `"authentication.kubernetes.io/node-name":["node-a"],"authentication.kubernetes.io/node-name":["node-a"]`, 1), nodecontext.Authentication},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := decodeIdentity(t.Context(), []byte(test.body), "node-a")
			assertReason(t, err, test.reason)
		})
	}
}

func TestRecreatedNodeCannotUseOldAuthenticatedUID(t *testing.T) {
	h := newHarness(t, func(http.ResponseWriter, *http.Request) { t.Error("contacted a recreated Node with the old identity") },
		func(node *corev1.Node) { node.UID = "replacement-node-uid" })
	_, err := h.source.Read(t.Context())
	assertReason(t, err, nodecontext.InvalidTarget)
}

func TestConfiguredNodeCannotOverrideAuthenticatedNode(t *testing.T) {
	h := newHarness(t, func(_ http.ResponseWriter, _ *http.Request) { t.Error("contacted kubelet for an unattested target") })
	opts := h.opts
	opts.NodeName = "node-b"
	source, err := New(h.config, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	_, err = source.Read(t.Context())
	assertReason(t, err, nodecontext.InvalidTarget)
}
