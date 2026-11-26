package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestChangedBuildInputsCannotBeSigned(t *testing.T) {
	build := os.Getenv("KML_FILECACHE_OBJECTS")
	if build == "" {
		t.Skip("requires offline candidate objects")
	}
	for _, change := range []string{"source", "second-object", "upstream", "duplicate-object", "unreproduced", "path-escape", "sdk-patch"} {
		t.Run(change, func(t *testing.T) {
			root := t.TempDir()
			if err := os.CopyFS(root, os.DirFS(build)); err != nil {
				t.Fatal(err)
			}
			patch := "../../worker/sdk-policy.patch"
			data, err := os.ReadFile(filepath.Join(root, "build.json"))
			if err != nil {
				t.Fatal(err)
			}
			var record buildRecord
			if json.Unmarshal(data, &record) != nil {
				t.Fatal("fixture build record invalid")
			}
			switch change {
			case "source":
				if os.WriteFile(filepath.Join(root, "source/common.h"), []byte("changed"), 0600) != nil {
					t.Fatal("fixture change failed")
				}
			case "second-object":
				if os.WriteFile(filepath.Join(root, "files-amd64-2/program.bpf.o"), []byte("changed"), 0600) != nil {
					t.Fatal("fixture change failed")
				}
			case "upstream":
				record.UpstreamCommit = "unreviewed"
			case "duplicate-object":
				record.Objects[1] = record.Objects[0]
			case "unreproduced":
				record.Objects[0].Reproduced = false
			case "path-escape":
				record.Objects[0].Path = "../../other-object"
			case "sdk-patch":
				patch = filepath.Join(root, "patch")
				if os.WriteFile(patch, []byte("changed"), 0600) != nil {
					t.Fatal("fixture change failed")
				}
			}
			encoded, err := json.Marshal(record)
			if err != nil || os.WriteFile(filepath.Join(root, "build.json"), encoded, 0600) != nil {
				t.Fatal("fixture record write failed")
			}
			if _, err := readInputs(root, patch); err == nil {
				t.Fatal("changed inputs accepted for signing")
			}
		})
	}
}
