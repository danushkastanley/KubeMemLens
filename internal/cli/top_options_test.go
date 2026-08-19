package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

func TestPodRowsApplyKubernetesLabelAndFieldSelectors(t *testing.T) {
	pods := []api.PodSnapshot{
		{Namespace: "default", PodName: "api", NodeName: "node-a", Context: api.PodContext{Phase: "Running", Labels: map[string]string{"app": "api"}}, Memory: model.MemoryBreakdown{TotalBytes: 200}},
		{Namespace: "default", PodName: "worker", NodeName: "node-b", Context: api.PodContext{Phase: "Pending", Labels: map[string]string{"app": "worker"}}, Memory: model.MemoryBreakdown{TotalBytes: 100}},
	}
	labelSelector, fieldSelector, err := parseSelectors("app=api", "spec.nodeName=node-a,status.phase=Running")
	if err != nil {
		t.Fatalf("parseSelectors returned error: %v", err)
	}
	rows := podRows(pods, labelSelector, fieldSelector)
	if len(rows) != 1 || rows[0].Name != "api" {
		t.Fatalf("unexpected selected rows: %#v", rows)
	}
}

func TestParseSelectorsRejectsUnsupportedField(t *testing.T) {
	if _, _, err := parseSelectors("", "metadata.uid=secret"); err == nil || !strings.Contains(err.Error(), "unsupported field") {
		t.Fatalf("error = %v, want unsupported field", err)
	}
}

func TestWriteTopRowsStructuredOutputOmitsLabelsAndRuntimeIDs(t *testing.T) {
	row := topRow{Namespace: "default", Name: "api", Kind: "Pod", TotalBytes: 100, Diagnosis: "normal", Confidence: "low"}
	for _, output := range []string{"json", "yaml", "csv"} {
		t.Run(output, func(t *testing.T) {
			buffer := &bytes.Buffer{}
			if err := writeTopRows(buffer, []topRow{row}, topOptions{Output: output, SortBy: "total"}, printPodRows); err != nil {
				t.Fatalf("writeTopRows returned error: %v", err)
			}
			for _, forbidden := range []string{"containerID", "cgroupPath", "labels"} {
				if strings.Contains(buffer.String(), forbidden) {
					t.Fatalf("%s output contains %q: %s", output, forbidden, buffer.String())
				}
			}
			if !strings.Contains(buffer.String(), "api") {
				t.Fatalf("%s output missing row: %s", output, buffer.String())
			}
		})
	}
}

func TestWriteTopRowsSortsAndOmitsHeaders(t *testing.T) {
	rows := []topRow{{Name: "small", TotalBytes: 1}, {Name: "large", TotalBytes: 2}}
	buffer := &bytes.Buffer{}
	if err := writeTopRows(buffer, rows, topOptions{Output: "table", SortBy: "total", NoHeaders: true}, printPodRows); err != nil {
		t.Fatalf("writeTopRows returned error: %v", err)
	}
	if strings.Contains(buffer.String(), "NAMESPACE") || strings.Index(buffer.String(), "large") > strings.Index(buffer.String(), "small") {
		t.Fatalf("unexpected table output: %s", buffer.String())
	}
}

func TestTopRowsExposeAndSortPrimaryResidual(t *testing.T) {
	rows := []topRow{{Name: "small-other", ResidualBytes: 1}, {Name: "large-other", ResidualBytes: 2}}
	buffer := &bytes.Buffer{}
	if err := writeTopRows(buffer, rows, topOptions{Output: "csv", SortBy: "residual"}, printPodRows); err != nil {
		t.Fatalf("writeTopRows returned error: %v", err)
	}
	if !strings.Contains(buffer.String(), "residual_bytes") || strings.Index(buffer.String(), "large-other") > strings.Index(buffer.String(), "small-other") {
		t.Fatalf("unexpected residual output: %s", buffer.String())
	}
}

func TestWatchTopStopsWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := watchTop(ctx, &bytes.Buffer{}, topOptions{Watch: true, WatchInterval: time.Second}, func() error {
		calls++
		cancel()
		return nil
	})
	if err != nil || calls != 1 {
		t.Fatalf("watchTop = %v, calls=%d; want nil, 1", err, calls)
	}
}
