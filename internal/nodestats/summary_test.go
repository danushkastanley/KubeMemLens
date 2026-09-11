package nodestats

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/nodecontext"
)

func TestRealSourcePreservesUnitsSourcesAndPrivacy(t *testing.T) {
	h := newHarness(t, successHandler(t))
	report, err := h.source.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if report.NodeName != "node-a" || report.NodeUID != "node-uid-a" || report.Stats.Provenance != nodecontext.Unknown {
		t.Fatal(report)
	}
	memory := report.Stats.Memory
	if *memory.UsageBytes != 1000 || memory.PSI.Some.TotalNanoseconds != 1500 || !memory.CapturedAt.Equal(sampleNow) {
		t.Fatal(memory)
	}
	if report.Stats.Swap.UsageBytes == nil || *report.Stats.Swap.UsageBytes != 0 {
		t.Fatal("reported zero lost")
	}
	if report.Availability != capability.Available || report.Evidence.Completeness != capability.Partial || report.Evidence.Freshness != capability.Fresh {
		t.Fatal(report.Evidence)
	}
	if *report.Context.CapacityBytes != 8<<30 || *report.Context.Hugepages[0].CapacityBytes != 512<<20 || *report.Context.Hugepages[0].AllocatableBytes != 256<<20 {
		t.Fatal(report.Context)
	}
	encoded, _ := json.Marshal(report)
	for _, secret := range []string{"private-pod", "other-tenant", "private-volume", "/private/path", "private-annotation", "api-credential", "node-credential"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("uncollected data retained: %s", secret)
		}
	}
	if h.config.DisableCompression || h.config.Proxy != nil {
		t.Fatal("caller transport configuration mutated")
	}
}

func TestOptionalFieldsAndSourceAge(t *testing.T) {
	for _, fields := range []string{`"memory":{"time":"2026-09-11T12:00:00Z","usageBytes":0}`, `"memory":null,"swap":null`} {
		body := `{"node":{"nodeName":"node-a","startTime":"2026-09-11T11:00:00Z",` + fields + `}}`
		parsed, err := decodeSummary(t.Context(), []byte(body), "node-a", sampleNow)
		if err != nil {
			t.Fatal(err)
		}
		report := observation(nodeTarget{name: "node-a"}, parsed, sampleNow.Add(time.Minute))
		if parsed.stats.Memory == nil {
			if report.Availability != capability.Unreported || report.Stats != nil || report.Evidence.Freshness != capability.UnknownFreshness {
				t.Fatal(report)
			}
			continue
		}
		if parsed.stats.Memory.AvailableBytes != nil || parsed.stats.Memory.PSI != nil || *parsed.stats.Memory.UsageBytes != 0 || report.Evidence.Freshness != capability.Stale {
			t.Fatal(report)
		}
	}
}

func TestMalformedSummaryIsRejected(t *testing.T) {
	base := summaryBody()
	for name, body := range map[string]string{
		"wrong-node":   strings.Replace(base, `"node-a"`, `"node-b"`, 1),
		"duplicate":    strings.Replace(base, `"usageBytes":1000`, `"usageBytes":1000,"usageBytes":20`, 1),
		"negative":     strings.Replace(base, `"usageBytes":1000`, `"usageBytes":-1`, 1),
		"overflow":     strings.Replace(base, `"usageBytes":1000`, `"usageBytes":18446744073709551616`, 1),
		"working-set":  strings.Replace(base, `"workingSetBytes":600`, `"workingSetBytes":2000`, 1),
		"missing-time": strings.Replace(base, `"time":"2026-09-11T12:00:00Z",`, ``, 1),
		"future":       strings.ReplaceAll(base, `12:00:00Z`, `12:01:00Z`),
		"old":          strings.ReplaceAll(base, `12:00:00Z`, `11:55:00Z`),
		"bad-psi":      strings.Replace(base, `"avg10":1.2`, `"avg10":101`, 1),
		"missing-psi":  strings.Replace(base, `"avg10":1.2,`, ``, 1),
		"truncated":    base[:len(base)-1], "trailing": base + ` {}`, "null": `null`,
		"deep-skipped": strings.TrimSuffix(base, "}") + `,"ignored":` + strings.Repeat(`[`, 33) + `0` + strings.Repeat(`]`, 33) + `}`,
		"token-budget": strings.TrimSuffix(base, "}") + `,"ignored":[` + strings.Repeat(`0,`, nodecontext.MaxJSONTokens) + `0]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeSummary(t.Context(), []byte(body), "node-a", sampleNow); err == nil {
				t.Fatal("accepted invalid summary")
			}
		})
	}
}

func TestSystemCategoriesAreBoundedAndUnique(t *testing.T) {
	body := `{"node":{"nodeName":"node-a","startTime":"2026-09-11T11:00:00Z","systemContainers":[{"name":"pods","startTime":"2026-09-11T11:00:00Z","memory":{"time":"2026-09-11T12:00:00Z","usageBytes":10}},{"name":"private-new-system"}]}}`
	parsed, err := decodeSummary(t.Context(), []byte(body), "node-a", sampleNow)
	if err != nil || len(parsed.stats.SystemContainers) != 1 || parsed.stats.SystemContainers[0].Category != nodecontext.Pods || !parsed.partial {
		t.Fatalf("%+v %v", parsed, err)
	}
	encoded, _ := json.Marshal(parsed.stats)
	if strings.Contains(string(encoded), "private-new-system") {
		t.Fatal("unknown system name retained")
	}
	duplicate := strings.Replace(body, `"private-new-system"`, `"pods"`, 1)
	if _, err := decodeSummary(t.Context(), []byte(duplicate), "node-a", sampleNow); err == nil {
		t.Fatal("duplicate category accepted")
	}
}

func FuzzSummary(f *testing.F) {
	f.Add([]byte(summaryBody()))
	f.Add([]byte(`{"node":null}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		value, err := decodeSummary(context.Background(), data, "node-a", sampleNow)
		if err == nil && len(value.stats.SystemContainers) > nodecontext.MaxSystemContainers {
			t.Fatal("unbounded categories")
		}
	})
}
