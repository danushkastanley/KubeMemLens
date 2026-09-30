package main

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/recommend"
)

func fixtures(t *testing.T) (Corpus, []Prediction, map[string]string) {
	t.Helper()
	raw, err := os.ReadFile("testdata/corpus-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	corpus, err := loadCorpus(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	copies, err := os.ReadFile("testdata/copy-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var pins map[string]string
	if err := json.Unmarshal(copies, &pins); err != nil {
		t.Fatal(err)
	}
	var predictions []Prediction
	for _, c := range corpus.Cases {
		p, err := predict(c)
		if err != nil {
			t.Fatal(c.ID, err)
		}
		predictions = append(predictions, p)
	}
	return corpus, predictions, pins
}
func TestFrozenCorpusPassesThroughProductionRules(t *testing.T) {
	corpus, predictions, pins := fixtures(t)
	report, err := evaluate(corpus, predictions, pins)
	if err != nil || !report.Passed {
		t.Fatalf("%+v %v", report, err)
	}
	for _, family := range report.Families {
		if family.Cases != family.Passed {
			t.Fatal(family)
		}
	}
	if report.Sources["synthetic"] != 49 || report.Sources["local-cluster"] != 7 {
		t.Fatal("invented provenance")
	}
	for _, m := range report.Rules {
		if m.TP == 0 || m.FP != 0 || m.FN != 0 || m.Precision == nil || *m.Precision != 1 || m.Recall == nil || *m.Recall != 1 {
			t.Fatal(m)
		}
	}
}
func TestWrongAdviceAndLostEvidenceCannotPass(t *testing.T) {
	for _, kind := range []string{"wrong-diagnosis", "wrong-confidence", "false-negative", "prohibited", "unknown", "missing-guard", "changed-action", "missing-rationale", "wrong-abstention"} {
		t.Run(kind, func(t *testing.T) {
			c, p, pins := fixtures(t)
			switch kind {
			case "wrong-diagnosis":
				p[0].Diagnosis = "normal"
			case "wrong-confidence":
				p[0].Confidence = "low"
			case "false-negative":
				p[0].Recommendations = p[0].Recommendations[1:]
			case "prohibited":
				p[0].Recommendations = append(p[0].Recommendations, recommend.Recommendation{ID: "observe-bounded-history"})
			case "unknown":
				p[0].Recommendations = append(p[0].Recommendations, recommend.Recommendation{ID: "private-unapproved-command"})
			case "missing-guard":
				p[0].Recommendations = p[0].Recommendations[:1]
			case "changed-action":
				p[0].Recommendations[0].Action = "Delete the workload."
			case "missing-rationale":
				p[0].Recommendations[0].Rationale = ""
			case "wrong-abstention":
				p[0].Abstain = true
			}
			r, err := evaluate(c, p, pins)
			if err != nil || r.Passed || r.Cases[0].Passed {
				t.Fatal(kind, r, err)
			}
			raw, _ := json.Marshal(r)
			if bytes.Contains(raw, []byte("private-unapproved-command")) || bytes.Contains(raw, []byte("Delete the workload")) {
				t.Fatal("untrusted advice entered aggregate")
			}
		})
	}
}
func TestMetricsRetainFalsePositivesAndUndefinedPrecision(t *testing.T) {
	c, p, pins := fixtures(t)
	// Withhold every cache recommendation; precision is undefined, recall is zero.
	for i := range p {
		p[i].Recommendations = slices.DeleteFunc(p[i].Recommendations, func(r recommend.Recommendation) bool { return r.ID == "inspect-file-cache" })
	}
	p[0].Recommendations = append(p[0].Recommendations, recommend.Recommendation{ID: "observe-bounded-history", Action: "observe", Rationale: "unsupported", Conditions: []string{"none"}})
	r, err := evaluate(c, p, pins)
	if err != nil {
		t.Fatal(err)
	}
	m := r.Rules["inspect-file-cache"]
	if m.Precision != nil || m.Recall == nil || *m.Recall != 0 || m.FN == 0 {
		t.Fatal(m)
	}
	if r.Rules["observe-bounded-history"].FP != 1 || r.Cases[0].ProhibitedAdvice != 1 || r.Passed {
		t.Fatal(r)
	}
}
func TestMissingCoverageAndDuplicateOutputsFail(t *testing.T) {
	c, p, pins := fixtures(t)
	p[0].Recommendations = append(p[0].Recommendations, p[0].Recommendations[0])
	if _, err := evaluate(c, p, pins); err == nil {
		t.Fatal("duplicate output accepted")
	}
	c, p, pins = fixtures(t)
	c.Cases = c.Cases[:1]
	p = p[:1]
	pins = map[string]string{c.Cases[0].ID: pins[c.Cases[0].ID]}
	r, err := evaluate(c, p, pins)
	if err != nil || r.Passed || len(r.MissingCoverage) == 0 {
		t.Fatal(r, err)
	}
}
func TestRunnerIsDeterministicAndDoesNotExportInputs(t *testing.T) {
	raw, err := os.ReadFile("testdata/corpus-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	pins, err := os.ReadFile("testdata/copy-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var outputs [2]bytes.Buffer
	for i := range outputs {
		if err := run(bytes.NewReader(raw), bytes.NewReader(pins), &outputs[i]); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(outputs[0].Bytes(), outputs[1].Bytes()) {
		t.Fatal("nondeterministic report")
	}
	for _, s := range []string{"fixture-runtime", "fixture-workload", "fixture-node", "fixture-pod", "namespace", "receiptSHA256", "recommendations"} {
		if strings.Contains(outputs[0].String(), s) {
			t.Fatal("input or identity disclosed", s)
		}
	}
}
func TestEveryProductionRecommendationHasTaxonomyAndCases(t *testing.T) {
	files, err := filepath.Glob("../../internal/recommend/*.go")
	if err != nil {
		t.Fatal(err)
	}
	discovered := map[string]bool{}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			var value ast.Expr
			switch n := node.(type) {
			case *ast.KeyValueExpr:
				if key, ok := n.Key.(*ast.Ident); ok && key.Name == "ID" {
					value = n.Value
				}
			case *ast.AssignStmt:
				if len(n.Lhs) == 1 && len(n.Rhs) == 1 {
					if key, ok := n.Lhs[0].(*ast.SelectorExpr); ok && key.Sel.Name == "ID" {
						value = n.Rhs[0]
					}
				}
			}
			if value != nil {
				literal, ok := value.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					t.Fatal("recommendation ID requires explicit inventory review")
				}
				id, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatal(err)
				}
				discovered[id] = true
			}
			return true
		})
	}
	catalog := map[string]bool{}
	for _, rule := range rules {
		catalog[rule.ID] = true
		if rule.Evidence == "" {
			t.Fatal("unmapped evidence")
		}
	}
	if !reflect.DeepEqual(discovered, catalog) {
		t.Fatalf("production %v taxonomy %v", discovered, catalog)
	}
}
