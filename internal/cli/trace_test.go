package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/client"
)

func TestTraceCLIUsesOneAdmissionAndExplicitExport(t *testing.T) {
	f := traceCommandFixture(t, "success")
	file := filepath.Join(t.TempDir(), "report.json")
	f.command.SetArgs([]string{"run", "selected-pod", "-n", "tenant-a", "--container", "worker", "--kind", "files", "--export", file, "--confirm-export"})
	if err := f.command.Execute(); err != nil {
		t.Fatal(err)
	}
	if f.creates.Load() != 1 || f.streams.Load() != 1 || f.deletes.Load() != 1 {
		t.Fatal("incorrect operation counts")
	}
	for _, want := range []string{"state: completed", "cleanup: confirmed", "incomplete evidence: true", "engine produced: 0"} {
		if !strings.Contains(f.out.String(), want) {
			t.Fatal("missing summary", want, f.out.String())
		}
	}
	data, err := os.ReadFile(file)
	if err != nil || !json.Valid(data) {
		t.Fatal("invalid export", err)
	}
	for _, private := range []string{"private-pod-uid", "private-node-uid", traceTestID, "selected-pod", "worker"} {
		if strings.Contains(string(data), private) {
			t.Fatal("export disclosed identity", private)
		}
	}
}
func TestTraceCLIRequiresConsentBeforeAnyRequests(t *testing.T) {
	f := traceCommandFixture(t, "success")
	f.command.SetArgs([]string{"run", "selected-pod", "--container", "worker", "--kind", "files", "--export", "-"})
	if err := f.command.Execute(); err == nil || !strings.Contains(err.Error(), "confirm-export") {
		t.Fatal("missing consent", err)
	}
	if f.creates.Load() != 0 || f.streams.Load() != 0 {
		t.Fatal("activated without consent")
	}
}
func TestTraceCLICreateWatchAndCancelContracts(t *testing.T) {
	for _, action := range []string{"preflight", "create", "watch", "cancel", "export"} {
		t.Run(action, func(t *testing.T) {
			f := traceCommandFixture(t, "success")
			args := []string{action, "selected-pod", "-n", "tenant-a", "--container", "worker", "--kind", "files"}
			if action == "watch" || action == "cancel" || action == "export" {
				args = []string{action, traceTestID, "-n", "tenant-a"}
			}
			if action == "export" {
				args = append(args, "--export", "-", "--confirm-export")
			}
			f.command.SetArgs(args)
			if err := f.command.Execute(); err != nil {
				t.Fatal(err)
			}
			switch action {
			case "preflight":
				if !json.Valid(f.out.Bytes()) || f.creates.Load() != 0 || f.streams.Load() != 0 {
					t.Fatal("preflight activated")
				}
			case "create":
				if f.creates.Load() != 1 || f.streams.Load() != 0 || !strings.Contains(f.out.String(), traceTestID) {
					t.Fatal("create did not produce pending admission")
				}
			case "watch", "export":
				if f.creates.Load() != 0 || f.streams.Load() != 1 || f.deletes.Load() != 1 {
					t.Fatal("watch recreated or failed cleanup")
				}
			case "cancel":
				if f.creates.Load() != 0 || f.streams.Load() != 0 || f.deletes.Load() != 1 {
					t.Fatal("cancel activated")
				}
			}
			if action == "export" && !json.Valid(f.out.Bytes()) {
				t.Fatal("progress interleaved with JSON", f.out.String())
			}
		})
	}
}
func TestTraceCLIRejectsIncompleteUnavailableAndUnknownCleanup(t *testing.T) {
	for _, mode := range []string{"partial", "absent", "gone"} {
		t.Run(mode, func(t *testing.T) {
			f := traceCommandFixture(t, mode)
			args := []string{"watch", traceTestID, "-n", "tenant-a"}
			if mode == "gone" {
				args[0] = "cancel"
			}
			f.command.SetArgs(args)
			if err := f.command.Execute(); err == nil {
				t.Fatal("failure became success")
			}
			if mode == "gone" && !strings.Contains(f.out.String(), "cleanup: unconfirmed") {
				t.Fatal("absence became cleanup")
			}
			if mode == "absent" && f.streams.Load() != 0 {
				t.Fatal("missing extension activated")
			}
		})
	}
}
func TestTraceRejectsUnrelatedConnectionModes(t *testing.T) {
	for _, opts := range []client.Options{{Mode: client.ConnectionModeHTTP}, {Mode: client.ConnectionModeKubeProxy}, {EvidenceMode: capability.Restricted}, {CollectorURL: "http://127.0.0.1:8080"}} {
		if _, err := openTraceClient(opts); err == nil {
			t.Fatal("accepted unrelated connection")
		}
	}
}
