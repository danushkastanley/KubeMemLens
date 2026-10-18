package main

import (
	"github.com/danushkastanley/kube-memlens/internal/recommend"
)

// All inputs are numerical or closed vocabulary. Domain adapters supply only
// fixed synthetic identities; exports never include an incident's identifiers.
type Corpus struct {
	SchemaVersion int    `json:"schemaVersion"`
	Cases         []Case `json:"cases"`
}
type Case struct {
	ID         string     `json:"id"`
	Family     string     `json:"family"`
	Provenance Provenance `json:"provenance"`
	Memory     *Memory    `json:"memory,omitempty"`
	Volume     *Volume    `json:"volume,omitempty"`
	Replica    *Replica   `json:"replica,omitempty"`
	Expected   Expected   `json:"expected"`
}
type Provenance struct {
	Source        string `json:"source"`
	Consent       string `json:"consent"`
	Sanitisation  string `json:"sanitisation"`
	ReceiptSHA256 string `json:"receiptSHA256,omitempty"`
}
type Memory struct {
	Charge                uint64   `json:"charge"`
	Anon                  uint64   `json:"anon"`
	File                  uint64   `json:"file"`
	Shmem                 uint64   `json:"shmem"`
	Slab                  uint64   `json:"slab"`
	SlabUnreclaimable     uint64   `json:"slabUnreclaimable"`
	Dirty                 uint64   `json:"dirty"`
	Writeback             uint64   `json:"writeback"`
	Socket                uint64   `json:"socket"`
	PageTables            uint64   `json:"pageTables"`
	Min                   *uint64  `json:"min,omitempty"`
	Low                   *uint64  `json:"low,omitempty"`
	High                  *uint64  `json:"high,omitempty"`
	Max                   *uint64  `json:"max,omitempty"`
	OOM                   uint64   `json:"oom"`
	OOMKill               uint64   `json:"oomKill"`
	MaxEvents             uint64   `json:"maxEvents"`
	HighEvents            uint64   `json:"highEvents"`
	DeltasKnown           bool     `json:"deltasKnown"`
	LocalEvents           bool     `json:"localEvents"`
	PSISome               *float64 `json:"psiSome,omitempty"`
	PSIFull               *float64 `json:"psiFull,omitempty"`
	TerminationAgeSeconds *int     `json:"terminationAgeSeconds,omitempty"`
	Stale                 bool     `json:"stale"`
}
type Volume struct {
	Capacity     *uint64 `json:"capacity,omitempty"`
	Used         *uint64 `json:"used,omitempty"`
	Available    *uint64 `json:"available,omitempty"`
	Inodes       *uint64 `json:"inodes,omitempty"`
	InodesFree   *uint64 `json:"inodesFree,omitempty"`
	MemoryBacked bool    `json:"memoryBacked"`
	AgeSeconds   int     `json:"ageSeconds"`
	Health       string  `json:"health"`
}
type Replica struct {
	Charges        []uint64 `json:"charges"`
	ReferenceState string   `json:"referenceState"`
	ChangeHistory  string   `json:"changeHistory"`
}
type Expected struct {
	Diagnosis  string   `json:"diagnosis"`
	Confidence string   `json:"confidence"`
	Checks     []string `json:"checks"`
	Prohibited []string `json:"prohibited"`
	Abstain    bool     `json:"abstain"`
}
type Prediction struct {
	Diagnosis       string
	Confidence      string
	Recommendations []recommend.Recommendation
	Abstain         bool
}
type Rule struct {
	ID       string `json:"id"`
	Family   string `json:"family"`
	Evidence string `json:"evidence"`
	Safety   bool   `json:"safety"`
}

// The corpus must include positive and negative labels for each non-safety rule.
var rules = []Rule{
	{"investigate-oom-evidence", "memory", "Recent OOM/hard-limit evidence, cumulative counter caveat, or recent recorded termination.", false},
	{"investigate-reclaim-impact", "memory", "Known PSI or recent high-event delta; retain composition and scope.", false},
	{"validate-limit-headroom", "memory", "Known finite hard limit and measured low headroom without stronger evidence.", false},
	{"profile-anonymous-memory", "memory", "Anonymous charge dominates; no assertion that it is a leak.", false},
	{"inspect-file-cache", "memory", "File cache excluding shmem; pressure/refault limitations retained.", false},
	{"bound-memory-backed-storage", "memory", "Shared-memory charge; ownership and workload contract required before changes.", false},
	{"inspect-kernel-facing-behaviour", "memory", "Measured slab, socket or page-table charge.", false},
	{"inspect-writeback-path", "memory", "Measured dirty/writeback charge; storage cause not asserted.", false},
	{"observe-bounded-history", "memory", "No unique dominant signal; absence of evidence is not healthy history.", false},
	{"no-automatic-mutation", "memory", "Required read-only guard for every primary memory recommendation.", true},
	{"inspect-filesystem-headroom", "volume", "Reported filesystem bytes/inodes; retain source time and separate memory charge.", false},
	{"inspect-source-health", "volume", "Adverse health observation; preserve scope and conflicting reports.", false},
	{"inspect-tmpfs-attribution", "volume", "Memory-backed configuration and shared-memory evidence, no per-mount attribution.", false},
	{"inspect-storage-memory-window", "volume", "Writeback, I/O stalls or comparable cache movement, without causality.", false},
	{"inspect-memory-qos-evidence", "qos", "Observed controls/delta window or explicit evidence-quality reason to collect again.", false},
}
