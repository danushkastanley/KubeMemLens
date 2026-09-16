package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestProcessIdentityChecksExecutableAndLifetime(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	p, err := openProcess(os.Getpid(), digest, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	if p.check() != nil {
		t.Fatal("own process lifetime was not verified")
	}
	if _, err := openProcess(os.Getpid(), digest, p.start+1); err == nil {
		t.Fatal("different process lifetime accepted")
	}
	if _, err := openProcess(os.Getpid(), strings.Repeat("0", 64), p.start); err == nil {
		t.Fatal("different executable accepted")
	}
	if selectedTarget(os.Getpid(), map[uint64]bool{1: true}) {
		t.Fatal("ordinary test descriptor became a selected cgroup")
	}
}

func TestProcStartParsingHandlesCommandDelimiters(t *testing.T) {
	fields := append([]string{"S"}, strings.Fields(strings.Repeat("0 ", 18))...)
	fields = append(fields, "123", "0")
	value, err := startTime([]byte("12 (command ) with spaces) " + strings.Join(fields, " ")))
	if err != nil || value != 123 {
		t.Fatal("command delimiters changed lifetime identity")
	}
	for _, data := range []string{"", "12 no delimiter", "12 (x) S 1", "12 (x) " + strings.Repeat("0 ", 20)} {
		if _, err := startTime([]byte(data)); err == nil {
			t.Fatal("incomplete or zero process start accepted")
		}
	}
}

func TestContainerMembershipRequiresFullUnambiguousRuntimeIdentity(t *testing.T) {
	id := strings.Repeat("a", 64)
	for _, path := range []string{"/kubelet.slice/worker/cri-containerd-" + id + ".scope", "/kubepods/worker/" + id} {
		got, err := containerMembership([]byte("0::"+path+"\n"), id)
		if err != nil || got != path {
			t.Fatal("exact runtime cgroup rejected")
		}
	}
	for _, data := range []string{"0::/cri-containerd-" + id[:12] + ".scope\n", "0::/other\n", "0::/../" + id, "0::/" + id + "\n0::/other", "1:memory:/" + id} {
		if _, err := containerMembership([]byte(data), id); err == nil {
			t.Fatal("another or ambiguous container lifetime accepted")
		}
	}
}

func TestTargetAndCapturedObjectInputsRejectFalseZeroes(t *testing.T) {
	for _, input := range []string{"", "0", "1,1", "1,2,3", "01", "-1", "1, 2"} {
		if _, err := targetIDs(input); err == nil {
			t.Fatal("ambiguous target accepted")
		}
	}
	if ids, err := targetIDs("12,34"); err != nil || len(ids) != 2 {
		t.Fatal("two exact targets rejected")
	}
	for _, input := range []string{`{"link":[],"map":[1],"map":[],"prog":[]}`, `{"link":[],"map":[],"prog":[]} {}`, strings.Repeat(" ", 16385)} {
		if _, err := decodeIDs([]byte(input)); err == nil {
			t.Fatal("ambiguous object record accepted")
		}
	}
	for _, ids := range []objectIDs{{}, {"unknown": {}}, {"link": {}, "map": nil, "prog": {}}, {"link": {}, "map": {}, "wrong": {}}} {
		if _, err := remaining(ids); err == nil {
			t.Fatal("missing object inventory became measured zero")
		}
	}
	data, _ := json.Marshal(objectIDs{"map": {}, "prog": {}, "link": {}})
	ids, err := decodeIDs(data)
	if err != nil {
		t.Fatal(err)
	}
	left, err := remaining(ids)
	if err != nil || len(left) != 3 {
		t.Fatal("explicit empty inventory rejected")
	}
}
