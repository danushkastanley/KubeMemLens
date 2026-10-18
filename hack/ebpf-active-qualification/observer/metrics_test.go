package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func agentFixture() string {
	var b strings.Builder
	for key := range agentFields {
		value := "1"
		if key == "last_scan_duration_seconds" {
			value = "0.003000001"
		}
		if key == "last_scan_timestamp_seconds" {
			value = "1790670000"
		}
		fmt.Fprintf(&b, "kubememlens_agent_%s %s\n", key, value)
	}
	b.WriteString("# EOF\n")
	return b.String()
}
func collectorFixture() string {
	return "kubememlens_collector_ingestion_last_received_timestamp_seconds 0\nkubememlens_collector_ingestion_last_received_age_seconds 0\nkubememlens_collector_ingestion_requests_total{result=\"accepted\"} 2\nkubememlens_collector_ingestion_last_duration_seconds 1.0000001e-3\n# EOF\n"
}
func TestProjectionPreservesPrecisionAndDiscardsIdentityFamilies(t *testing.T) {
	raw := strings.Replace(agentFixture(), "# EOF", "unrelated{node=\"private-node\"} 1\n# EOF", 1)
	a, err := parseAgent([]byte(raw))
	if err != nil || a["scanDurationNanos"] != 3000001 || a["scanCompletedUnixSeconds"] != 1790670000 {
		t.Fatal(a, err)
	}
	c, err := parseCollector([]byte(collectorFixture()))
	if err != nil || c.DurationNanos != 1000001 || c.Results["accepted"] != 2 || c.Results["identity_rejected"] != 0 {
		t.Fatal(c, err)
	}
	encoded, _ := json.Marshal([]any{a, c})
	if strings.Contains(string(encoded), "private-node") {
		t.Fatal("identity exported")
	}
}
func TestMissingInvalidAndAmbiguousMetricsFail(t *testing.T) {
	for _, raw := range []string{strings.Replace(agentFixture(), "# EOF\n", "", 1), "# EOF\n" + agentFixture(), strings.Replace(agentFixture(), "metadata_cache_pods 1\n", "", 1), strings.Replace(agentFixture(), "# EOF", "kubememlens_agent_metadata_cache_pods 1\n# EOF", 1), strings.Replace(agentFixture(), `result="success"`, `result="unknown"`, 1), string([]byte{0xff}) + agentFixture()} {
		if _, err := parseAgent([]byte(raw)); err == nil {
			t.Fatal("incomplete or ambiguous metric accepted")
		}
	}
	for _, value := range []string{"0", "NaN", "+Inf", "-1", "1e999", "1800.000000001"} {
		if _, err := parseAgent([]byte(strings.Replace(agentFixture(), "0.003000001", value, 1))); err == nil {
			t.Fatal(value)
		}
	}
	for _, value := range []string{"1.5", "9007199254740992"} {
		if _, err := parseAgent([]byte(strings.Replace(agentFixture(), "metadata_cache_pods 1", "metadata_cache_pods "+value, 1))); err == nil {
			t.Fatal(value)
		}
	}
	for _, raw := range []string{"# EOF\n", "kubememlens_collector_ingestion_last_duration_seconds 1\n# EOF\n", strings.Replace(collectorFixture(), `result="accepted"`, `result="unknown"`, 1)} {
		if _, err := parseCollector([]byte(raw)); err == nil {
			t.Fatal("missing collector family became zero")
		}
	}
}
func TestMetricBoundsAndExactConversion(t *testing.T) {
	if value, err := metricValue("1800", true); err != nil || value != 1800000000000 {
		t.Fatal(value, err)
	}
	if value, err := metricValue("9007199254740991", false); err != nil || value != 9007199254740991 {
		t.Fatal(value, err)
	}
	if _, err := metricValue("1.0000000000000000001", false); err == nil {
		t.Fatal("fraction rounded to integer")
	}
	if _, err := parseAgent([]byte(strings.Repeat("# x\n", 8193) + agentFixture())); err == nil {
		t.Fatal("too many lines")
	}
	if _, err := parseAgent([]byte(strings.Repeat("x", 1<<20) + agentFixture())); err == nil {
		t.Fatal("oversized")
	}
}
