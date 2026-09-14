package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNoticesRequireLicenceAndRetainAdditionalNotices(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("module"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := notices(root); err == nil {
		t.Fatal("module with no notices accepted")
	}
	if err := os.WriteFile(filepath.Join(root, "NOTICE"), []byte("notice only"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := notices(root); err == nil {
		t.Fatal("notice without licence accepted")
	}
	for _, name := range []string{"LICENSE", "NOTICE", "PATENTS"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	names, err := notices(root)
	if err != nil || !reflect.DeepEqual(names, []string{"LICENSE", "NOTICE", "PATENTS"}) {
		t.Fatalf("incomplete notices: %v %v", names, err)
	}
}
