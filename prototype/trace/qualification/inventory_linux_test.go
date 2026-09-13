package qualification

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"os"
	"sort"
	"testing"
)

// TestAdministratorInventory is an explicitly enabled test-only observer for an
// owned disposable Linux host. Linux requires SYS_ADMIN for global enumeration.
// It is never linked into the prototype command or deployed as a helper.
func TestAdministratorInventory(t *testing.T) {
	if os.Getenv("KML_ADMIN_INVENTORY") != "owned-local-test-node" {
		t.Skip("administrator test observer requires explicit opt-in")
	}
	ids := []string{}
	var prog ebpf.ProgramID
	for {
		next, err := ebpf.ProgramGetNextID(prog)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		prog = next
		ids = append(ids, fmt.Sprintf("programme/%d", prog))
		if len(ids) > 4096 {
			t.Fatal("inventory bound exceeded")
		}
	}
	var m ebpf.MapID
	for {
		next, err := ebpf.MapGetNextID(m)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		m = next
		ids = append(ids, fmt.Sprintf("map/%d", m))
		if len(ids) > 4096 {
			t.Fatal("inventory bound exceeded")
		}
	}
	iter := link.Iterator{}
	defer iter.Close()
	for iter.Next() {
		info, err := iter.Link.Info()
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, fmt.Sprintf("link/%d", info.ID))
		if len(ids) > 4096 {
			t.Fatal("inventory bound exceeded")
		}
	}
	if err := iter.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(ids)
	data, err := json.Marshal(ids)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("BPF_INVENTORY=%s\n", data)
}
