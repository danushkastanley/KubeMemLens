package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/explain"
)

func TestRecommendationDocumentDisablesAutomaticMutation(t *testing.T) {
	document := recommendationOutput(explanationTarget{Kind: "Pod", Namespace: "default", Name: "api"}, explain.Result{Diagnosis: explain.DiagnosisRSSHeavy, Confidence: explain.ConfidenceMedium})
	if document.AutomaticMutation || len(document.Recommendations) < 2 {
		t.Fatalf("unexpected recommendation document: %#v", document)
	}
	for _, output := range []string{"text", "json", "yaml"} {
		buffer := &bytes.Buffer{}
		if err := writeRecommendationDocument(buffer, output, document); err != nil {
			t.Fatalf("write %s: %v", output, err)
		}
		if !strings.Contains(strings.ToLower(buffer.String()), "automatic") || !strings.Contains(buffer.String(), "profile-anonymous-memory") {
			t.Fatalf("unexpected %s output: %s", output, buffer.String())
		}
	}
}
