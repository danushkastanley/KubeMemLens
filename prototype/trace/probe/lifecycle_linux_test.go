package probe

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cilium/ebpf"
	p "github.com/danushkastanley/kube-memlens/internal/tracepreflight"
)

// Opt-in integration test: run only in a disposable container with a private
// writable /run/kube-memlens-tracer and BPF/PERFMON, never on an existing service.
func TestLiveEndedWorkerDescriptorIsReportedWithoutDeletion(t *testing.T) {
	if os.Getenv("KML_DESCRIPTOR_TEST") != "owned-local-test-worker" {
		t.Skip("requires disposable worker")
	}
	if _, err := os.Lstat(endedReceiptPath); !os.IsNotExist(err) {
		t.Fatal("receipt path already exists or is inaccessible")
	}
	ring, err := ebpf.NewMap(&ebpf.MapSpec{Name: "kml_pf_orphan", Type: ebpf.RingBuf, MaxEntries: uint32(os.Getpagesize())})
	if err != nil {
		t.Fatal("cannot create bounded diagnostic map")
	}
	defer ring.Close()
	info, err := ring.Info()
	if err != nil {
		t.Fatal(err)
	}
	id, ok := info.ID()
	if !ok {
		t.Fatal("map ID unavailable")
	}
	boot, err := readBounded("/proc/sys/kernel/random/boot_id", 64)
	if err != nil {
		t.Fatal(err)
	}
	start, err := processStart(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	receipt := EndedReceipt{SchemaVersion: 1, BootID: strings.TrimSpace(string(boot)), EndedAt: time.Now().Add(-time.Second), OwnerPID: os.Getpid(), OwnerStartTicks: start, Objects: []EndedObject{{FD: ring.FD(), Kind: "map", ID: uint32(id)}}}
	data, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(endedReceiptPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	local := New("")
	if check := local.ownership(context.Background()); check.State != p.Unsupported || check.Reason != p.OwnedOrphan {
		t.Fatalf("orphan: %+v", check)
	}
	// Scanning must preserve even the object it has positively identified.
	if _, err := ring.Info(); err != nil {
		t.Fatal("scan deleted or closed the owned map")
	}
	receipt.OwnerStartTicks++
	data, _ = json.Marshal(receipt)
	if err := os.WriteFile(endedReceiptPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	if check := local.ownership(context.Background()); check.Reason != p.OwnershipUncertain {
		t.Fatal("process lifetime mismatch accepted")
	}
	if err := ring.Close(); err != nil {
		t.Fatal(err)
	}
	receipt.OwnerStartTicks--
	data, _ = json.Marshal(receipt)
	if err := os.WriteFile(endedReceiptPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	if check := New("").ownership(context.Background()); check.State != p.Supported {
		t.Fatalf("closed map left state: %+v", check)
	}
}
