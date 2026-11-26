package verifier

import (
	_ "embed"
	"encoding/json"
	"strings"
	"testing"
)

//go:embed testdata/linuxkit-7.0.12-arm64.json
var nativeFormats []byte

// These schemas came from a bounded register/read/remove cycle of disabled
// probes. They establish real layout compatibility, not argument-fetch coverage.
func TestRegisteredLinuxKernelFormatsMatchNumericProjection(t *testing.T) {
	var fixture struct {
		Owner   string            `json:"owner"`
		Formats map[string]string `json:"formats"`
	}
	if err := json.Unmarshal(nativeFormats, &fixture); err != nil {
		t.Fatal(err)
	}
	plan, err := NewProbePlan(fixture.Owner)
	if err != nil || len(fixture.Formats) != 3 {
		t.Fatal("invalid native fixture")
	}
	for role, kind := range map[string]Kind{"enter": CheckEnter, "return": CheckReturn, "log": LogFinalized} {
		name := plan.group + "/" + role + "_" + fixture.Owner
		format, err := plan.ParseFormat(fixture.Formats[name], kind)
		if err != nil || format.kind != kind {
			t.Fatal("actual kernel schema rejected")
		}
		if _, err := plan.ParseFormat(strings.Replace(fixture.Formats[name], "common_pid", "other_pid", 1), kind); err == nil {
			t.Fatal("changed native task binding accepted")
		}
	}
}
