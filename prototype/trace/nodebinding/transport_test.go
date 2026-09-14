package nodebinding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	admission "github.com/danushkastanley/kube-memlens/internal/traceadmission"
)

func TestMutualTLSLifecycleAndReplay(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	w := workload()
	expires := time.Now().Add(5 * time.Second)
	id := strings.Repeat("a", 32)
	b, err := f.client.Bind(ctx, id, w, expires)
	if err != nil {
		t.Fatal(err)
	}
	h := <-f.handles
	if b.Target().CgroupID != 123 || b.Target().PodUID != w.Target.PodUID {
		t.Fatal("binding changed target")
	}
	if err := b.Revalidate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.client.Bind(ctx, id, w, expires); !errors.Is(err, admission.ErrExpired) {
		t.Fatalf("replay: %v", err)
	}
	if err := b.Close(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.closed:
	default:
		t.Fatal("node handle leaked")
	}
	if err := b.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := b.Revalidate(ctx); !errors.Is(err, admission.ErrExpired) {
		t.Fatalf("closed binding: %v", err)
	}
	if _, err := f.client.Bind(ctx, id, w, expires); !errors.Is(err, admission.ErrExpired) {
		t.Fatalf("cancelled nonce reused: %v", err)
	}
}

func TestUntrustedControlAndNodeCertificates(t *testing.T) {
	f := setup(t)
	other, otherPeer := f.certs.issue(t)
	for _, tc := range []struct {
		name          string
		forgedControl bool
	}{{"control", true}, {"node", false}} {
		t.Run(tc.name, func(t *testing.T) {
			identity, peer := f.control, f.node
			if tc.forgedControl {
				identity = other
			} else {
				peer = otherPeer
			}
			c, err := NewClient(identity, []Endpoint{{"node-uid", "node", f.server.URL, peer}})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if _, err := c.Bind(context.Background(), strings.Repeat("b", 32), workload(), time.Now().Add(time.Second)); !errors.Is(err, admission.ErrUnavailable) {
				t.Fatalf("forged peer accepted: %v", err)
			}
		})
	}
	if len(f.handles) != 0 {
		t.Fatal("node work began for forged peer")
	}
}

func TestServerRejectsNodeSubstitutionAndWireAliases(t *testing.T) {
	f := setup(t)
	n := f.client.nodes["node-uid"]
	original := requestFor(strings.Repeat("c", 32), workload(), time.Now().Add(time.Second))
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	cases := [][]byte{
		bytes.Replace(data, []byte(`"node-uid"`), []byte(`"other-node"`), 1),
		bytes.Replace(data, []byte(`"nodeUID"`), []byte(`"NodeUID"`), 1),
		append([]byte(`{"id":"duplicate",`), data[1:]...),
		append([]byte(`{"gadget":"arbitrary",`), data[1:]...),
	}
	for _, data := range cases {
		response, err := n.call(context.Background(), http.MethodPost, "/v1/bindings", data)
		if response != nil {
			response.Body.Close()
		}
		if err == nil {
			t.Fatal("invalid private request accepted")
		}
	}
	if len(f.handles) != 0 {
		t.Fatal("node work began for invalid request")
	}
}

func TestNodeQuotasPrecedeTargetWork(t *testing.T) {
	f := setup(t)
	for _, id := range []string{"a", "b"} {
		if _, err := f.client.Bind(context.Background(), strings.Repeat(id, 32), workload(), time.Now().Add(time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.client.Bind(context.Background(), strings.Repeat("c", 32), workload(), time.Now().Add(time.Second)); !errors.Is(err, admission.ErrCapacity) {
		t.Fatalf("quota: %v", err)
	}
	if len(f.handles) != 2 {
		t.Fatal("quota allowed extra node work")
	}
}
