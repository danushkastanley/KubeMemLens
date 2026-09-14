package traceadmission

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestConfiguredBoundsAndDisclosureApplyBeforeResolution(t *testing.T) {
	for _, field := range []string{`"durationSeconds":31`, `"maxEvents":10001`, `"maxOutputBytes":8388609`, `"maxMapBytes":8388609`, `"maxPathBytes":257`, `"rawPaths":true`} {
		h := newHarness(t, DefaultPolicy())
		body := strings.TrimSuffix(validRequest, "}") + "," + field + "}"
		r, err := DecodeRequest("tenant-a", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.manager.Admit(context.Background(), actor("user-a"), r); err == nil {
			t.Fatal("caller raised configured policy")
		}
		if h.resolver.calls.Load() != 0 || h.binder.calls.Load() != 0 {
			t.Fatal("policy rejection performed target work")
		}
	}
}

func TestNamespaceAndGlobalQuotasCountAllRequesters(t *testing.T) {
	for _, scope := range []string{"namespace", "global"} {
		t.Run(scope, func(t *testing.T) {
			policy := DefaultPolicy()
			if scope == "global" {
				policy.Global = 2
			}
			policy.PerNode = 2
			h := newHarness(t, policy)
			for i, name := range []string{"user-a", "user-b"} {
				namespace := "tenant-a"
				if scope == "global" && i == 1 {
					namespace = "tenant-b"
				}
				if _, err := h.manager.Admit(context.Background(), actor(name), requestFor(t, namespace)); err != nil {
					t.Fatal(err)
				}
			}
			namespace := "tenant-a"
			if scope == "global" {
				namespace = "tenant-c"
			}
			if _, err := h.manager.Admit(context.Background(), actor("user-c"), requestFor(t, namespace)); !errors.Is(err, ErrCapacity) {
				t.Fatal("quota exceeded")
			}
			if h.resolver.calls.Load() != 2 || h.binder.calls.Load() != 2 {
				t.Fatal("over-quota request reached resolver or node")
			}
		})
	}
}

func TestPrincipalSnapshotRetainsClaimsWithoutSharingMutableStorage(t *testing.T) {
	p := actor("user-a")
	copied, key, err := snapshotPrincipal(p)
	if err != nil {
		t.Fatal(err)
	}
	p.Name = "changed"
	p.UID = "changed"
	p.Groups[0] = "changed"
	p.Extra["scope"][0] = "changed"
	if copied.GetName() != "user-a" || copied.GetUID() != "uid-user-a" || copied.GetGroups()[0] == "changed" || copied.GetExtra()["scope"][0] == "changed" {
		t.Fatal("principal snapshot shared mutable claims")
	}
	_, again, err := snapshotPrincipal(actor("user-a"))
	if err != nil || key != again {
		t.Fatal("stable requester identity changed")
	}
	p = actor("user-a")
	p.Groups = append(p.Groups, "new-group")
	_, again, err = snapshotPrincipal(p)
	if err != nil || key != again {
		t.Fatal("group changes created another quota bucket")
	}
	p = actor("user-a")
	p.Extra["large"] = make([]string, 33)
	if _, _, err := snapshotPrincipal(p); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("unbounded identity accepted")
	}
}
