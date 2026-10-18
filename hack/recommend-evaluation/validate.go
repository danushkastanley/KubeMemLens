package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"regexp"
	"slices"
	"strings"
)

const corpusFields = "|schemaVersion|cases|id|family|provenance|memory|volume|replica|expected|source|consent|sanitisation|receiptSHA256|charge|anon|file|shmem|slab|slabUnreclaimable|dirty|writeback|socket|pageTables|min|low|high|max|oom|oomKill|maxEvents|highEvents|deltasKnown|localEvents|psiSome|psiFull|terminationAgeSeconds|stale|capacity|used|available|inodes|inodesFree|memoryBacked|ageSeconds|health|charges|referenceState|changeHistory|diagnosis|confidence|checks|prohibited|abstain|evidence|safety|"

var errCorpus = errors.New("invalid or unbounded recommendation corpus")
var fixtureID = regexp.MustCompile(`^(memory|volume|qos|replica)-[a-z0-9-]{1,64}$`)
var hashID = regexp.MustCompile(`^[0-9a-f]{64}$`)

func loadCorpus(input io.Reader) (Corpus, error) {
	var corpus Corpus
	raw, err := io.ReadAll(io.LimitReader(input, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return corpus, errCorpus
	}
	// Reject duplicate JSON keys as well as unknown schema fields.
	scanner := json.NewDecoder(bytes.NewReader(raw))
	if err := uniqueJSON(scanner, 0); err != nil {
		return corpus, errCorpus
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&corpus) != nil || d.Decode(new(any)) != io.EOF {
		return corpus, errCorpus
	}
	return corpus, validateCorpus(corpus)
}

func uniqueJSON(d *json.Decoder, depth int) error {
	if depth > 8 {
		return errCorpus
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || len(name) > 96 || seen[strings.ToLower(name)] || (!strings.Contains(corpusFields, "|"+name+"|") && !fixtureID.MatchString(name)) {
				return errCorpus
			}
			seen[strings.ToLower(name)] = true
			if err := uniqueJSON(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := uniqueJSON(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return errCorpus
	}
	_, err = d.Token()
	return err
}

func validateCorpus(c Corpus) error {
	if c.SchemaVersion != 1 || len(c.Cases) == 0 || len(c.Cases) > 256 {
		return errCorpus
	}
	seen := map[string]bool{}
	for _, item := range c.Cases {
		if !fixtureID.MatchString(item.ID) || seen[item.ID] {
			return errCorpus
		}
		seen[item.ID] = true
		p := item.Provenance
		if p.Sanitisation != "numeric-allowlist-v1" {
			return errCorpus
		}
		switch p.Source {
		case "synthetic":
			if p.Consent != "not-applicable" || p.ReceiptSHA256 != "" {
				return errCorpus
			}
		case "local-cluster", "managed-provider":
			if p.Consent != "owned-fixture" || !hashID.MatchString(p.ReceiptSHA256) {
				return errCorpus
			}
		case "consented-incident":
			if p.Consent != "explicit-recorded" || !hashID.MatchString(p.ReceiptSHA256) {
				return errCorpus
			}
		default:
			return errCorpus
		}
		if err := validateCase(item); err != nil {
			return err
		}
	}
	return nil
}

func validateCase(c Case) error {
	switch c.Family {
	case "memory", "qos":
		if c.Memory == nil || c.Volume != nil || c.Replica != nil {
			return errCorpus
		}
	case "volume":
		if c.Memory == nil || c.Volume == nil || c.Replica != nil {
			return errCorpus
		}
	case "replica":
		if c.Memory != nil || c.Volume != nil || c.Replica == nil {
			return errCorpus
		}
	default:
		return errCorpus
	}
	if c.Memory != nil {
		m := c.Memory
		for _, n := range []uint64{m.Charge, m.Anon, m.File, m.Shmem, m.Slab, m.SlabUnreclaimable, m.Dirty, m.Writeback, m.Socket, m.PageTables, m.OOM, m.OOMKill, m.MaxEvents, m.HighEvents} {
			if n > 1<<52 {
				return errCorpus
			}
		}
		for _, n := range []*uint64{m.Min, m.Low, m.High, m.Max} {
			if n != nil && *n > 1<<52 {
				return errCorpus
			}
		}
		for _, n := range []*float64{m.PSISome, m.PSIFull} {
			if n != nil && (math.IsNaN(*n) || math.IsInf(*n, 0) || *n < 0 || *n > 100) {
				return errCorpus
			}
		}
		if n := m.TerminationAgeSeconds; n != nil && (*n < -60 || *n > 86400) {
			return errCorpus
		}
		if (c.Memory.PSISome == nil) != (c.Memory.PSIFull == nil) {
			return errCorpus
		}
	}
	if v := c.Volume; v != nil {
		if v.AgeSeconds < 0 || v.AgeSeconds > 600 || !slices.Contains([]string{"unreported", "healthy", "adverse", "conflicting"}, v.Health) {
			return errCorpus
		}
		for _, n := range []*uint64{v.Capacity, v.Used, v.Available, v.Inodes, v.InodesFree} {
			if n != nil && *n > 1<<52 {
				return errCorpus
			}
		}
		if (v.Used != nil || v.Available != nil) && v.Capacity == nil || v.InodesFree != nil && v.Inodes == nil {
			return errCorpus
		}
	}
	if r := c.Replica; r != nil {
		if !slices.Contains([]string{"current-stable", "unreported"}, r.ChangeHistory) || len(r.Charges) < 2 || len(r.Charges) > 16 || !slices.Contains([]string{"current", "stale", "partial", "revision-mismatch"}, r.ReferenceState) {
			return errCorpus
		}
		for _, n := range r.Charges {
			if n > 1<<52 {
				return errCorpus
			}
		}
	}
	return validateLabels(c)
}

func validateLabels(c Case) error {
	diagnoses := map[string][]string{
		"memory":  {"oom-risk", "memory-pressure", "limit-risk", "rss-heavy", "cache-heavy", "tmpfs-heavy", "slab-heavy", "dirty-writeback-heavy", "mixed", "normal"},
		"volume":  {"storage-info", "storage-high"},
		"qos":     {"observed", "unavailable", "stale", "resize-unsettled", "potentially-stale-or-inconsistent"},
		"replica": {"replica-none", "replica-higher", "replica-lower", "replica-insufficient-peers"},
	}
	if !slices.Contains(diagnoses[c.Family], c.Expected.Diagnosis) || !slices.Contains([]string{"high", "medium", "low", "not-assessed", "current-peers", "limited-change-history", "insufficient"}, c.Expected.Confidence) {
		return errCorpus
	}
	seen := map[string]bool{}
	for _, list := range [][]string{c.Expected.Checks, c.Expected.Prohibited} {
		for _, id := range list {
			if seen[id] {
				return errCorpus
			}
			seen[id] = true
			found := false
			for _, rule := range rules {
				if rule.ID == id && rule.Family == c.Family {
					found = true
				}
			}
			if !found {
				return errCorpus
			}
		}
	}
	if c.Family == "memory" && !slices.Contains(c.Expected.Checks, "no-automatic-mutation") {
		return errCorpus
	}
	return nil
}
