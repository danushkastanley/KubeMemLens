package probe

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	p "github.com/danushkastanley/kube-memlens/internal/tracepreflight"
)

func TestOwnedOrphanRequiresExactDescriptorIdentity(t *testing.T) {
	now := time.Now().UTC()
	receipt := EndedReceipt{SchemaVersion: 1, BootID: "boot-one", EndedAt: now.Add(-time.Second), OwnerPID: 123, OwnerStartTicks: 100, Objects: []EndedObject{{FD: 5, Kind: "prog", ID: 17, Tag: "0123456789abcdef"}}}
	data, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeEnded(data, "boot-one", now)
	if err != nil {
		t.Fatal(err)
	}
	objects := map[int]object{5: {kind: "prog", id: 17, tag: "0123456789abcdef"}, 6: {kind: "map", id: 99}}
	state, reason := evaluateEnded(decoded, objects)
	if state != p.Unsupported || reason != p.OwnedOrphan {
		t.Fatal("owned orphan not reported")
	}
	for _, boot := range []string{"boot-two", ""} {
		if _, err := decodeEnded(data, boot, now); err == nil {
			t.Fatal("boot mismatch accepted")
		}
	}
	objects[5] = object{kind: "prog", id: 18, tag: "0123456789abcdef"}
	if _, reason := evaluateEnded(decoded, objects); reason != p.OwnershipUncertain {
		t.Fatal("reused descriptor treated as owned")
	}
	delete(objects, 5)
	if _, reason := evaluateEnded(decoded, objects); reason != p.Available {
		t.Fatal("closed descriptor reported as orphan")
	}
	if len(objects) != 1 || objects[6].id != 99 {
		t.Fatal("audit changed unrelated descriptors")
	}
	receipt.Objects = append(receipt.Objects, receipt.Objects[0])
	data, _ = json.Marshal(receipt)
	if _, err := decodeEnded(data, "boot-one", now); err == nil {
		t.Fatal("duplicate descriptor accepted")
	}
}

func TestInvalidOwnershipRecordCannotEstablishOwnership(t *testing.T) {
	for _, data := range [][]byte{
		[]byte(`{"schemaVersion":1,"bootID":"boot-one","unknown":true}`),
		[]byte(`{"schemaVersion":1,"bootID":"boot-one"} {}`),
		[]byte(`{"schemaVersion":1,"bootID":"boot-one","endedAt":"2999-01-01T00:00:00Z"}`),
		make([]byte, 16<<10+1),
	} {
		if _, err := decodeEnded(data, "boot-one", time.Now()); err == nil {
			t.Fatal("invalid ownership receipt accepted")
		}
	}
}

func TestDescriptorInventoryKeepsOnlyBPFIdentity(t *testing.T) {
	tests := []struct {
		data    string
		want    object
		found   bool
		invalid bool
	}{
		{"pos:\t0\nflags:\t0100000\n", object{}, false, false},
		{"prog_id:\t42\nprog_tag:\t0123456789abcdef\nmemlock:\t4096\n", object{kind: "prog", id: 42, tag: "0123456789abcdef"}, true, false},
		{"map_id:\t7\nmap_type:\t27\n", object{kind: "map", id: 7}, true, false},
		{"prog_id:\t42\nlink_id:\t8\n", object{kind: "link", id: 8}, true, false},
		{"map_id:\t0\n", object{}, false, true},
		{"prog_id:\t1\nprog_tag:\tbad\n", object{}, false, true},
		{"link_id:\t8\nlink_id:\t9\n", object{}, false, true},
	}
	for _, tt := range tests {
		obj, found, err := descriptorObject([]byte(tt.data))
		if (err != nil) != tt.invalid || obj != tt.want || found != tt.found {
			t.Fatalf("unexpected descriptor parse: %v %v %v", obj, found, err)
		}
	}
}

func TestProcessStartHandlesParenthesesInComm(t *testing.T) {
	stat := "123 (worker ) with spaces)) S " + strings.Repeat("0 ", 18) + "456 0 0"
	if got, err := parseProcessStart([]byte(stat)); err != nil || got != 456 {
		t.Fatalf("start: %d %v", got, err)
	}
	for _, data := range []string{"", "123 (worker) S 0", strings.Replace(stat, "456", "invalid", 1)} {
		if _, err := parseProcessStart([]byte(data)); err == nil {
			t.Fatal("invalid stat accepted")
		}
	}
	if start, err := processStart(os.Getpid()); err != nil || start == 0 {
		t.Fatalf("live process start: %d %v", start, err)
	}
}

func TestOwnInventoryAndCleanupDoNotNeedBPFPrivileges(t *testing.T) {
	l := New("")
	before, err := snapshot(context.Background(), os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 0 {
		t.Fatal("unexpected BPF descriptors in unprivileged test")
	}
	l.before = before
	if check := l.cleanup(context.Background()); check.State != p.Supported {
		t.Fatalf("cleanup: %+v", check)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := snapshot(ctx, os.Getpid()); err == nil {
		t.Fatal("cancelled inventory proceeded")
	}
}

func TestOwnershipReceiptRejectsSymlinksAndPublicFiles(t *testing.T) {
	path := t.TempDir() + "/receipt.json"
	if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readEndedReceipt(path); err == nil {
		t.Fatal("public receipt accepted")
	}
	if err := os.Symlink(path, path+".link"); err != nil {
		t.Fatal(err)
	}
	if _, err := readEndedReceipt(path + ".link"); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestUnknownPinsAreReportedAndNeverRemoved(t *testing.T) {
	dir := t.TempDir()
	if clear, err := reservedPinsClear(dir); err != nil || !clear {
		t.Fatal("empty namespace rejected")
	}
	path := dir + "/unknown"
	if err := os.WriteFile(path, []byte("unrelated"), 0600); err != nil {
		t.Fatal(err)
	}
	if clear, err := reservedPinsClear(dir); err != nil || clear {
		t.Fatal("unknown pin ignored")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "unrelated" {
		t.Fatal("unknown state changed")
	}
	if err := os.Symlink(dir, dir+"-link"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(dir + "-link") })
	if _, err := reservedPinsClear(dir + "-link"); err == nil {
		t.Fatal("symlink pin namespace accepted")
	}
}
