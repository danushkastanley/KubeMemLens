package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
)

type Metrics struct {
	TP                int      `json:"truePositive"`
	FP                int      `json:"falsePositive"`
	FN                int      `json:"falseNegative"`
	TN                int      `json:"trueNegative"`
	EvidenceSupported int      `json:"evidenceSupported"`
	Precision         *float64 `json:"precision"`
	Recall            *float64 `json:"recall"`
	EvidenceCoverage  *float64 `json:"evidenceCoverage"`
}
type CaseResult struct {
	ID                  string   `json:"id"`
	Family              string   `json:"family"`
	Source              string   `json:"source"`
	DiagnosisMatched    bool     `json:"diagnosisMatched"`
	ConfidenceMatched   bool     `json:"confidenceMatched"`
	AbstentionMatched   bool     `json:"abstentionMatched"`
	Abstained           bool     `json:"abstained"`
	FalsePositives      []string `json:"falsePositives"`
	FalseNegatives      []string `json:"falseNegatives"`
	ProhibitedAdvice    int      `json:"prohibitedAdvice"`
	UnreviewedAdvice    int      `json:"unreviewedAdvice"`
	UnsupportedEvidence int      `json:"unsupportedEvidence"`
	MissingSafetyGuard  bool     `json:"missingSafetyGuard"`
	CopyMatched         bool     `json:"copyMatched"`
	Passed              bool     `json:"passed"`
}
type FamilyMetrics struct {
	Cases              int `json:"cases"`
	Passed             int `json:"passed"`
	Abstentions        int `json:"abstentions"`
	CorrectAbstentions int `json:"correctAbstentions"`
}
type Report struct {
	SchemaVersion   int                      `json:"schemaVersion"`
	TaxonomyVersion int                      `json:"taxonomyVersion"`
	CorpusSHA256    string                   `json:"corpusSHA256"`
	Passed          bool                     `json:"passed"`
	Cases           []CaseResult             `json:"cases"`
	Rules           map[string]Metrics       `json:"rules"`
	Families        map[string]FamilyMetrics `json:"families"`
	Sources         map[string]int           `json:"sources"`
	MissingCoverage []string                 `json:"missingCoverage"`
}

func ratio(n, d int) *float64 {
	if d == 0 {
		return nil
	}
	v := float64(n) / float64(d)
	return &v
}
func copyDigest(p Prediction) string {
	data, _ := json.Marshal(p.Recommendations)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// evaluate scores caller-supplied outputs without executing or changing rules.
// Frozen expectations define the finite corpus, not universal safety or accuracy.
func evaluate(corpus Corpus, predictions []Prediction, reviewedCopy map[string]string) (Report, error) {
	report := Report{SchemaVersion: 1, TaxonomyVersion: 1, Passed: true, Cases: []CaseResult{}, Rules: map[string]Metrics{}, Families: map[string]FamilyMetrics{}, Sources: map[string]int{}, MissingCoverage: []string{}}
	if err := validateCorpus(corpus); err != nil {
		return report, err
	}
	if len(predictions) != len(corpus.Cases) || len(reviewedCopy) != len(corpus.Cases) {
		return report, errCorpus
	}
	encoded, _ := json.Marshal(corpus)
	sum := sha256.Sum256(encoded)
	report.CorpusSHA256 = hex.EncodeToString(sum[:])
	for _, rule := range rules {
		report.Rules[rule.ID] = Metrics{}
	}
	for index, c := range corpus.Cases {
		if !hashID.MatchString(reviewedCopy[c.ID]) {
			return report, errCorpus
		}
		p := predictions[index]
		result := CaseResult{ID: c.ID, Family: c.Family, Source: c.Provenance.Source, DiagnosisMatched: p.Diagnosis == c.Expected.Diagnosis, ConfidenceMatched: p.Confidence == c.Expected.Confidence, AbstentionMatched: p.Abstain == c.Expected.Abstain, Abstained: p.Abstain, FalsePositives: []string{}, FalseNegatives: []string{}, CopyMatched: copyDigest(p) == reviewedCopy[c.ID]}
		actual := map[string]bool{}
		supported := map[string]bool{}
		for _, item := range p.Recommendations {
			if actual[item.ID] {
				return report, fmt.Errorf("duplicate recommendation in case %s", c.ID)
			}
			actual[item.ID] = true
			known := false
			for _, rule := range rules {
				if rule.ID == item.ID && rule.Family == c.Family {
					known = true
				}
			}
			if !known {
				result.UnreviewedAdvice++
				continue
			}
			supported[item.ID] = item.Action != "" && item.Rationale != "" && len(item.Conditions) > 0
			if !supported[item.ID] {
				result.UnsupportedEvidence++
			}
			if slices.Contains(c.Expected.Prohibited, item.ID) {
				result.ProhibitedAdvice++
			}
		}
		result.MissingSafetyGuard = c.Family == "memory" && !actual["no-automatic-mutation"]
		for _, rule := range rules {
			if rule.Family != c.Family {
				continue
			}
			want, got := slices.Contains(c.Expected.Checks, rule.ID), actual[rule.ID]
			m := report.Rules[rule.ID]
			switch {
			case want && got:
				m.TP++
				if supported[rule.ID] {
					m.EvidenceSupported++
				}
			case got:
				m.FP++
				result.FalsePositives = append(result.FalsePositives, rule.ID)
			case want:
				m.FN++
				result.FalseNegatives = append(result.FalseNegatives, rule.ID)
			default:
				m.TN++
			}
			report.Rules[rule.ID] = m
		}
		result.Passed = result.DiagnosisMatched && result.ConfidenceMatched && result.AbstentionMatched && result.CopyMatched && len(result.FalsePositives) == 0 && len(result.FalseNegatives) == 0 && result.ProhibitedAdvice == 0 && result.UnreviewedAdvice == 0 && result.UnsupportedEvidence == 0 && !result.MissingSafetyGuard
		report.Cases = append(report.Cases, result)
		family := report.Families[c.Family]
		family.Cases++
		if result.Passed {
			family.Passed++
		}
		if p.Abstain {
			family.Abstentions++
			if c.Expected.Abstain {
				family.CorrectAbstentions++
			}
		}
		report.Families[c.Family] = family
		report.Sources[c.Provenance.Source]++
		report.Passed = report.Passed && result.Passed
	}
	for _, rule := range rules {
		m := report.Rules[rule.ID]
		m.Precision = ratio(m.TP, m.TP+m.FP)
		m.Recall = ratio(m.TP, m.TP+m.FN)
		m.EvidenceCoverage = ratio(m.EvidenceSupported, m.TP+m.FP)
		report.Rules[rule.ID] = m
		if m.TP+m.FN == 0 || !rule.Safety && m.FP+m.TN == 0 {
			report.MissingCoverage = append(report.MissingCoverage, rule.ID)
		}
		if m.EvidenceSupported != m.TP {
			report.Passed = false
		}
	}
	for _, family := range []string{"memory", "volume", "qos", "replica"} {
		if report.Families[family].Cases == 0 {
			report.MissingCoverage = append(report.MissingCoverage, family)
		}
	}
	report.Passed = report.Passed && len(report.MissingCoverage) == 0
	return report, nil
}
