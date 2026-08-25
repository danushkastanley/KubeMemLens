package client

import "testing"

func TestReadScopeRequiresOneExplicitBoundary(t *testing.T) {
	scope, err := NamespaceScope("team-a")
	if err != nil {
		t.Fatalf("NamespaceScope returned error: %v", err)
	}
	if scope.Namespace != "team-a" || scope.AllNamespaces {
		t.Fatalf("scope = %#v", scope)
	}
	cluster := AllNamespacesScope()
	if !cluster.AllNamespaces || cluster.Namespace != "" {
		t.Fatalf("cluster scope = %#v", cluster)
	}
	if _, err := NamespaceScope(""); err == nil {
		t.Fatal("NamespaceScope accepted an empty namespace")
	}
	if err := (ReadScope{}).validate(); err == nil {
		t.Fatal("zero scope was accepted")
	}
	if _, err := (Options{ReadScope: ReadScope{Namespace: "bad/name"}}).WithDefaults(); err == nil {
		t.Fatal("Options accepted an invalid namespace scope")
	}
}
