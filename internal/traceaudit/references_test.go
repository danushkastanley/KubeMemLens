package traceaudit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func referenceFixture(t *testing.T) *References {
	t.Helper()
	r, err := NewReferences(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestReferencesAreKeyedOwnedAndDomainSeparated(t *testing.T) {
	key := bytes.Repeat([]byte{1}, 32)
	r, err := NewReferences(key)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := r.Actor("private-user", "private-uid")
	if err != nil {
		t.Fatal(err)
	}
	// Frozen cross-language vector for the versioned, length-prefixed encoding.
	if actor != "hmac-sha256:ea0db43f47d5d9548a400b1c5d0bd269afbe766bc0898e616d7cc4cb7b1ae571" || r.KeyID() != "sha256:72cd6e8422c407fb6d098690f1130b7ded7ec2f7f5e1d30bd9d521f015363793" {
		t.Fatal("audit reference encoding changed")
	}
	key[0] = 9
	same, _ := r.Actor("private-user", "private-uid")
	if same != actor {
		t.Fatal("key shares caller memory")
	}
	rotated, _ := NewReferences(key)
	other, _ := rotated.Actor("private-user", "private-uid")
	if actor == other || r.KeyID() == rotated.KeyID() {
		t.Fatal("rotation did not change references")
	}
	tenant, _ := r.Tenant("private-user")
	if tenant == actor {
		t.Fatal("actor and tenant domains overlap")
	}
	left, _ := r.Actor("ab", "c")
	right, _ := r.Actor("a", "bc")
	if left == right {
		t.Fatal("identity components are ambiguous")
	}
	if strings.Contains(actor, "private") || strings.Contains(fmt.Sprintf("%+v", r), r.KeyID()) {
		t.Fatal("private material exposed")
	}
	if _, err := json.Marshal(r); err == nil {
		t.Fatal("key owner serialised")
	}
}
func TestTargetReferenceBindsFullRuntimeLifetime(t *testing.T) {
	r := referenceFixture(t)
	target := trace.TargetIdentity{Namespace: "tenant-a", PodName: "private-pod", PodUID: "private-uid", ContainerName: "worker", ContainerID: strings.Repeat("a", 64), ContainerStartedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), NodeUID: "private-node", CgroupID: 42}
	original, err := r.Target(target)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*trace.TargetIdentity){
		func(t *trace.TargetIdentity) { t.PodUID = "replacement" },
		func(t *trace.TargetIdentity) { t.ContainerID = strings.Repeat("b", 64) },
		func(t *trace.TargetIdentity) { t.ContainerStartedAt = t.ContainerStartedAt.Add(time.Second) },
		func(t *trace.TargetIdentity) { t.NodeUID = "other-node" },
		func(t *trace.TargetIdentity) { t.CgroupID++ },
	} {
		changed := target
		change(&changed)
		value, err := r.Target(changed)
		if err != nil || value == original {
			t.Fatal("runtime lifetime was omitted", err)
		}
	}
	target.ContainerStartedAt = target.ContainerStartedAt.In(time.FixedZone("offset", 3600))
	same, err := r.Target(target)
	if err != nil || same != original {
		t.Fatal("timezone spelling changed identity")
	}
	target.CgroupID = 0
	if _, err := r.Target(target); err == nil {
		t.Fatal("unbound target accepted as observed")
	}
}
func TestInvalidReferenceInputsFailWithoutPrivateErrors(t *testing.T) {
	for _, size := range []int{0, 31, 33, 4096} {
		if _, err := NewReferences(make([]byte, size)); err != ErrInvalid {
			t.Fatal("invalid key size accepted")
		}
	}
	r := referenceFixture(t)
	for _, name := range []string{"", "private\nidentity", strings.Repeat("x", 1025)} {
		if _, err := r.Actor(name, ""); err != ErrInvalid {
			t.Fatal("invalid actor accepted")
		}
	}
	if _, err := r.Tenant("invalid/namespace"); err != ErrInvalid {
		t.Fatal("invalid tenant accepted")
	}
	if _, err := r.Session("private-session"); err != ErrInvalid {
		t.Fatal("invalid session accepted")
	}
	var absent *References
	if _, err := absent.Actor("user", ""); err != ErrInvalid {
		t.Fatal("absent key accepted")
	}
}
