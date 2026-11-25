package collector

import "testing"

// A continue token is client-supplied. Any string must be refused or decode to
// a key that produces exactly the same token again for the same scope, so a
// token cannot be accepted in two spellings or continue a different scope.
func FuzzDecodeContinueToken(f *testing.F) {
	scope := "pods:cluster"
	valid := encodeScopedContainerCursor(scope, "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
	for _, seed := range []string{"", valid, valid + "x", valid[:len(valid)-1], "v2.", "v2...", "v9." + valid[3:], "\x00\xff"} {
		f.Add(seed, scope)
	}
	f.Fuzz(func(t *testing.T, token, tokenScope string) {
		if len(token) > 1024 || len(tokenScope) > 256 {
			return
		}
		key, err := decodeScopedContainerCursor(token, tokenScope)
		if err != nil {
			return
		}
		if token == "" {
			if key != "" {
				t.Fatalf("an empty token decoded to key %q", key)
			}
			return
		}
		if again := encodeScopedContainerCursor(tokenScope, key); again != token {
			t.Fatalf("token %q for scope %q decoded to a key that encodes as %q", token, tokenScope, again)
		}
	})
}
