package collector

import (
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

func firstPageToken(t *testing.T, scope string) string {
	t.Helper()
	page, err := PaginateKeys([]string{"a", "b", "c"}, url.Values{"limit": {"1"}}, scope)
	if err != nil || page.Continue == "" {
		t.Fatalf("first page: %v, token %q", err, page.Continue)
	}
	return page.Continue
}

func continueWith(token, scope string) error {
	_, err := PaginateKeys([]string{"a", "b", "c"}, url.Values{"limit": {"1"}, "continue": {token}}, scope)
	return err
}

// A client that was listing when the collector restarted holds a token from
// the old generation. It must be told to start again, which is what "expired"
// means, and not that its request was malformed.
func TestContinueTokenFromAnotherGenerationIsExpiredNotInvalid(t *testing.T) {
	token := firstPageToken(t, GenerationScope("pods:cluster", "generation-1"))

	if err := continueWith(token, GenerationScope("pods:cluster", "generation-1")); err != nil {
		t.Fatalf("token refused in its own generation: %v", err)
	}
	if err := continueWith(token, GenerationScope("pods:cluster", "generation-2")); !errors.Is(err, ErrContinueExpired) {
		t.Fatalf("token from an earlier generation: err = %v, want expired", err)
	}
	// A token replayed against a view it was not issued for is a bad request
	// in every generation. It must not be reported as merely expired.
	for _, generation := range []string{"generation-1", "generation-2"} {
		err := continueWith(token, GenerationScope("pods:namespace/team-a", generation))
		if err == nil || errors.Is(err, ErrContinueExpired) {
			t.Fatalf("token replayed against another scope in %s: err = %v, want invalid", generation, err)
		}
	}
}

func TestContinueTokenInTheEarlierFormatIsExpired(t *testing.T) {
	// The format before this one: "v2.", 16 digest characters, ".", 64 key characters.
	earlier := "v2." + strings.Repeat("a", 16) + "." + strings.Repeat("b", 64)
	if err := continueWith(earlier, GenerationScope("pods:cluster", "generation-1")); !errors.Is(err, ErrContinueExpired) {
		t.Fatalf("earlier-format token: err = %v, want expired", err)
	}
	for _, malformed := range []string{"v3", "v9." + strings.Repeat("a", 98), "not-a-token", firstPageToken(t, GenerationScope("pods:cluster", "g")) + "x"} {
		err := continueWith(malformed, GenerationScope("pods:cluster", "g"))
		if err == nil || errors.Is(err, ErrContinueExpired) {
			t.Fatalf("malformed token %q: err = %v, want invalid", malformed, err)
		}
	}
}

func TestNodeContextTokenFromAnotherGenerationIsExpired(t *testing.T) {
	now := time.Now().UTC()
	identities := map[string]string{"node-a": "uid-a", "node-b": "uid-b"}
	before := NewStore()
	if err := before.ReconcileNodeIdentities(identities, now); err != nil {
		t.Fatal(err)
	}
	_, token, err := before.PageNodeContexts(now, url.Values{"limit": {"1"}})
	if err != nil || token == "" {
		t.Fatalf("first page: %v, token %q", err, token)
	}
	// A new store is what a restarted collector has: the same Nodes, a new generation.
	after := NewStore()
	if err := after.ReconcileNodeIdentities(identities, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := after.PageNodeContexts(now, url.Values{"limit": {"1"}, "continue": {token}}); !errors.Is(err, ErrContinueExpired) {
		t.Fatalf("Node context token after a restart: err = %v, want expired", err)
	}
}
