package traceframe

import (
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
)

func TestMetadataCannotReplaceClaimIdentityPolicyProgrammeOrDeadline(t *testing.T) {
	frame := metadata(t)
	base := spec(t, trace.Files, trace.OmitPaths)
	id, engine, programme := strings.Repeat("b", 32), "sha256:"+strings.Repeat("c", 64), "sha256:"+strings.Repeat("d", 64)
	deadline := time.Unix(230, 0).UTC()
	if frame.MatchAdmission(id, engine, programme, base, deadline) != nil {
		t.Fatal("valid metadata rejected")
	}
	for _, change := range []string{"id", "engine", "programme", "deadline", "cgroup", "pod", "bounds", "paths"} {
		selectedID, selectedEngine, selectedProgramme, selectedDeadline := id, engine, programme, deadline
		target, bounds, paths := base.Target(), base.Bounds(), base.Paths()
		switch change {
		case "id":
			selectedID = strings.Repeat("a", 32)
		case "engine":
			selectedEngine = "sha256:" + strings.Repeat("a", 64)
		case "programme":
			selectedProgramme = "sha256:" + strings.Repeat("a", 64)
		case "deadline":
			selectedDeadline = deadline.Add(-time.Second)
		case "cgroup":
			target.CgroupID++
		case "pod":
			target.PodUID = "replacement"
		case "bounds":
			bounds.Events--
		case "paths":
			paths = trace.ConfirmedPaths
		}
		changed, err := trace.NewSpecification(trace.Files, target, paths, bounds)
		if err != nil {
			t.Fatal(err)
		}
		if frame.MatchAdmission(selectedID, selectedEngine, selectedProgramme, changed, selectedDeadline) == nil {
			t.Errorf("metadata accepted changed %s", change)
		}
	}
}
