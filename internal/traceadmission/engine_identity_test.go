package traceadmission

import (
	"context"
	"strings"
	"testing"
)

func TestAdmissionUsesFrozenInstallationEngineIdentity(t *testing.T) {
	policy := DefaultPolicy()
	policy.EngineDigest = "sha256:" + strings.Repeat("7", 64)
	want := policy.EngineDigest
	h := newHarness(t, policy)
	policy.EngineDigest = "sha256:" + strings.Repeat("8", 64)
	admitted, err := h.manager.Admit(context.Background(), actor("user-a"), requestFor(t, "tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	if admitted.EngineDigest() != want {
		t.Fatal("admission replaced installation-owned engine identity")
	}
	got, err := h.manager.Get(context.Background(), actor("user-a"), "tenant-a", admitted.ID())
	if err != nil || got.EngineDigest() != want {
		t.Fatal("engine identity changed after admission")
	}
}

func TestInvalidInstallationEngineIdentityIsRejected(t *testing.T) {
	for _, value := range []string{"", "sha256:short", "sha256:" + strings.Repeat("A", 64), "other:" + strings.Repeat("a", 64)} {
		policy := DefaultPolicy()
		policy.EngineDigest = value
		if policy.validate() == nil {
			t.Fatal("invalid installation identity accepted")
		}
	}
}
