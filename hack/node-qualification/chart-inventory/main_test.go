package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodePreservesResourceIdentityWithoutClusterAccess(t *testing.T) {
	input := "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: example, namespace: fixture}\n---\napiVersion: v1\nkind: Namespace\nmetadata: {name: fixture}\n"
	var output bytes.Buffer
	if err := decode(strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	var objects []map[string]any
	if err := json.Unmarshal(output.Bytes(), &objects); err != nil || len(objects) != 2 {
		t.Fatalf("unexpected inventory: %s, %v", output.Bytes(), err)
	}
	if objects[0]["metadata"].(map[string]any)["namespace"] != "fixture" {
		t.Fatal("namespace was changed")
	}
}

func TestDecodeRejectsMalformedOversizedAndExcessiveObjects(t *testing.T) {
	object := "apiVersion: v1\nkind: Namespace\nmetadata: {name: fixture}\n---\n"
	for _, input := range []string{"", "[]", "apiVersion: v1\nkind: Namespace\n", "private: [", strings.Repeat(object, 129), strings.Repeat("x", byteLimit+1)} {
		var output bytes.Buffer
		if err := decode(strings.NewReader(input), &output); err == nil || output.Len() != 0 {
			t.Fatal("invalid input accepted or partial output emitted")
		}
	}
}
