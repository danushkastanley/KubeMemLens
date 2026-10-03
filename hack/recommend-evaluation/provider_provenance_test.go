package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestManagedProviderSourceRemainsDistinctWithoutReceiptDisclosure(t *testing.T) {
	corpus, predictions, pins := fixtures(t)
	// Exercise the category with synthetic test data, not a claimed provider run.
	receipt := strings.Repeat("b", 64)
	corpus.Cases[0].Provenance = Provenance{Source: "managed-provider", Consent: "owned-fixture", Sanitisation: "numeric-allowlist-v1", ReceiptSHA256: receipt}
	report, err := evaluate(corpus, predictions, pins)
	if err != nil || !report.Passed {
		t.Fatalf("provider-labelled numerical evaluation failed: %v", err)
	}
	if report.Sources["managed-provider"] != 1 || report.Sources["local-cluster"] != 7 || report.Sources["synthetic"] != 48 || report.Cases[0].Source != "managed-provider" {
		t.Fatal("managed-provider provenance was lost or relabelled")
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(receipt)) || bytes.Contains(raw, []byte("receiptSHA256")) || bytes.Contains(raw, []byte("owned-fixture")) {
		t.Fatal("aggregate disclosed private receipt metadata")
	}
}
