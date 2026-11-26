package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBundleRetainsNoticesForCompiledPackageAncestors(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"go.mod":                                          "module example.net/app\n\ngo 1.27.0\nrequire example.net/dependency v0.0.0\nreplace example.net/dependency => ./dependency\n",
		"LICENSE":                                         "application licence",
		"cmd/memlens-trace/main.go":                       "package main\nimport _ \"example.net/dependency/third_party/forked/reader\"\nfunc main() {}\n",
		"dependency/go.mod":                               "module example.net/dependency\n\ngo 1.27.0\n",
		"dependency/LICENSE":                              "module licence",
		"dependency/third_party/forked/LICENSE":           "forked package licence",
		"dependency/third_party/forked/PATENTS":           "forked package patents",
		"dependency/third_party/forked/reader/NOTICE.md":  "reader notice",
		"dependency/third_party/forked/reader/license.go": "package reader\n",
		"dependency/unimported/LICENSE":                   "unrelated package licence",
	}
	for name, data := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)
	t.Setenv("GOWORK", "off")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOTOOLCHAIN", "local")
	output := filepath.Join(root, "bundle")
	if err := run(output); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(output, "inventory.json"))
	if err != nil {
		t.Fatal(err)
	}
	var inventory map[string][]string
	if err := json.Unmarshal(data, &inventory); err != nil {
		t.Fatal(err)
	}
	want := []string{"LICENSE", "third_party/forked/LICENSE", "third_party/forked/PATENTS", "third_party/forked/reader/NOTICE.md"}
	if got := inventory["example.net/dependency@v0.0.0"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("compiled-package notices = %v, want %v", got, want)
	}
	for _, name := range want {
		actual, err := os.ReadFile(filepath.Join(output, "example.net_dependency", name))
		if err != nil || string(actual) != files["dependency/"+name] {
			t.Fatalf("notice content %s was not retained: %v", name, err)
		}
	}
	if err := run(output); err == nil {
		t.Fatal("existing evidence was overwritten")
	}
}

func TestPackageNoticesRejectOutsideModuleAndIgnoreNoticeSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "LICENSE"), []byte("module licence"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := moduleNoticePaths(root, []string{outside}); err == nil {
		t.Fatal("package outside its declared module accepted")
	}
	if err := os.Symlink(outside, filepath.Join(root, "escaped")); err != nil {
		t.Fatal(err)
	}
	if _, err := moduleNoticePaths(root, []string{filepath.Join(root, "escaped")}); err == nil {
		t.Fatal("package directory symlink escaped module boundary")
	}
	if err := os.WriteFile(filepath.Join(outside, "NOTICE"), []byte("outside notice"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "NOTICE"), filepath.Join(root, "NOTICE")); err != nil {
		t.Fatal(err)
	}
	names, err := moduleNoticePaths(root, []string{root, root})
	if err != nil || !reflect.DeepEqual(names, []string{"LICENSE"}) {
		t.Fatalf("notice symlink copied or root duplicated: %v %v", names, err)
	}
}
