package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestLocalProjectionsRemainBoundToReviewedReceipts(t *testing.T) {
	corpus, _, _ := fixtures(t)
	for _, c := range corpus.Cases {
		if c.Provenance.Source != "local-cluster" {
			continue
		}
		raw, err := os.ReadFile("testdata/" + c.ID + "-receipt.json")
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(raw)
		if len(raw) > 16384 || hex.EncodeToString(digest[:]) != c.Provenance.ReceiptSHA256 {
			t.Fatal("receipt changed")
		}
		var receipt struct {
			SchemaVersion int      `json:"schemaVersion"`
			CaseID        string   `json:"caseID"`
			Method        string   `json:"method"`
			SourceSHA256  string   `json:"sourceSHA256"`
			CleanupSHA256 string   `json:"cleanupSHA256"`
			Memory        *Memory  `json:"memory"`
			Replica       *Replica `json:"replica"`
			Volume        *Volume  `json:"volume"`
		}
		if err := json.Unmarshal(raw, &receipt); err != nil {
			t.Fatal(err)
		}
		if receipt.SchemaVersion != 1 || receipt.CaseID != c.ID || receipt.Method == "" || !hashID.MatchString(receipt.SourceSHA256) || !hashID.MatchString(receipt.CleanupSHA256) || !reflect.DeepEqual(receipt.Memory, c.Memory) || !reflect.DeepEqual(receipt.Replica, c.Replica) || !reflect.DeepEqual(receipt.Volume, c.Volume) {
			t.Fatal("projection differs from reviewed receipt")
		}
	}
}
