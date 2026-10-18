package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestCorpusRejectsAmbiguousUnboundedAndPrivateInput(t *testing.T) {
	raw, err := os.ReadFile("testdata/corpus-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		string(raw) + " {}",
		strings.Replace(string(raw), `"charge": 104857600`, `"Charge": 104857600`, 1),
		strings.Repeat("[", 10) + strings.Repeat("]", 10),
		strings.Replace(string(raw), `"charge": 104857600`, `"charge": 104857600, "Charge": 1`, 1), strings.Replace(string(raw), `"schemaVersion": 1`, `"schemaVersion": 1, "schemaVersion": 1`, 1),
		strings.Replace(string(raw), `"charge": 104857600`, `"charge": 104857600, "namespace": "private"`, 1),
		strings.Replace(string(raw), `"charge": 104857600`, `"charge": -1`, 1),
		strings.Replace(string(raw), `"charge": 104857600`, `"charge": true`, 1),
		strings.Repeat(" ", 1<<20) + string(raw),
	} {
		if _, err := loadCorpus(strings.NewReader(bad)); err == nil {
			t.Fatal("invalid encoded corpus accepted")
		}
	}
}
func TestProvenanceAndLabelsAreBounded(t *testing.T) {
	for _, kind := range []string{"empty", "duplicate", "unknown-family", "missing-memory", "extra-volume", "unknown-rule", "duplicate-label", "consent", "unknown-source", "missing-receipt", "private-id", "invalid-psi", "partial-psi", "missing-safety", "invalid-count"} {
		t.Run(kind, func(t *testing.T) {
			c, _, _ := fixtures(t)
			switch kind {
			case "empty":
				c.Cases = nil
			case "duplicate":
				c.Cases = append(c.Cases, c.Cases[0])
			case "unknown-family":
				c.Cases[0].Family = "unknown"
			case "missing-memory":
				c.Cases[0].Memory = nil
			case "extra-volume":
				c.Cases[0].Volume = &Volume{}
			case "unknown-rule":
				c.Cases[0].Expected.Checks = append(c.Cases[0].Expected.Checks, "invented")
			case "duplicate-label":
				c.Cases[0].Expected.Prohibited = append(c.Cases[0].Expected.Prohibited, c.Cases[0].Expected.Checks[0])
			case "consent":
				c.Cases[0].Provenance.Consent = "assumed"
			case "unknown-source":
				c.Cases[0].Provenance.Source = "independent-review"
			case "missing-receipt":
				c.Cases[0].Provenance = Provenance{Source: "local-cluster", Consent: "owned-fixture", Sanitisation: "numeric-allowlist-v1"}
			case "private-id":
				c.Cases[0].ID = "/private/path"
			case "invalid-psi":
				v := 101.
				c.Cases[0].Memory.PSISome = &v
				c.Cases[0].Memory.PSIFull = &v
			case "partial-psi":
				v := 1.
				c.Cases[0].Memory.PSISome = &v
			case "missing-safety":
				c.Cases[0].Expected.Checks = c.Cases[0].Expected.Checks[:1]
			case "invalid-count":
				c.Cases = c.Cases[:1]
				c.Cases[0].Family = "replica"
				c.Cases[0].Memory = nil
				c.Cases[0].Replica = &Replica{Charges: []uint64{1}, ReferenceState: "current"}
			}
			raw, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := loadCorpus(bytes.NewReader(raw)); err == nil {
				t.Fatal("invalid corpus accepted")
			}
		})
	}
}

func TestManagedProviderProvenanceRequiresOwnedSanitisedReceipt(t *testing.T) {
	for _, kind := range []string{"valid", "missing-receipt", "invalid-receipt", "incident-consent", "assumed-ownership", "unsanitised"} {
		t.Run(kind, func(t *testing.T) {
			corpus, _, _ := fixtures(t)
			provenance := Provenance{Source: "managed-provider", Consent: "owned-fixture", Sanitisation: "numeric-allowlist-v1", ReceiptSHA256: strings.Repeat("a", 64)}
			switch kind {
			case "missing-receipt":
				provenance.ReceiptSHA256 = ""
			case "invalid-receipt":
				provenance.ReceiptSHA256 = "not-a-digest"
			case "incident-consent":
				provenance.Consent = "explicit-recorded"
			case "assumed-ownership":
				provenance.Consent = "assumed"
			case "unsanitised":
				provenance.Sanitisation = "raw"
			}
			corpus.Cases[0].Provenance = provenance
			raw, err := json.Marshal(corpus)
			if err != nil {
				t.Fatal(err)
			}
			_, err = loadCorpus(bytes.NewReader(raw))
			if (err == nil) != (kind == "valid") {
				t.Fatalf("provider provenance acceptance differs: %v", err)
			}
		})
	}
}
