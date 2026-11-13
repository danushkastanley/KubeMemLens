package cli

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
)

func limitTestPods() []api.PodSnapshot {
	return []api.PodSnapshot{
		{Namespace: "default", NodeName: "node-a", PodName: "finite", Memory: model.MemoryBreakdown{TotalBytes: 3 << 20, MaxKnown: true, MaxBytes: 64 << 20}},
		{Namespace: "default", NodeName: "node-a", PodName: "unlimited", Memory: model.MemoryBreakdown{TotalBytes: 2 << 20, MaxKnown: true, MaxUnlimited: true}},
		{Namespace: "default", NodeName: "node-a", PodName: "unread", Memory: model.MemoryBreakdown{TotalBytes: 1 << 20}},
	}
}

func limitTestRows() []topRow {
	return podRows(limitTestPods(), labels.Everything(), fields.Everything())
}

func TestTopTableSaysUnlimitedForAnUnlimitedLimit(t *testing.T) {
	var output bytes.Buffer
	if err := writeTopRows(&output, limitTestRows(), topOptions{Output: "table"}, printPodRows); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"finite": "64Mi", "unlimited": "unlimited", "unread": "unknown"}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	limitColumn := -1
	for index, header := range strings.Fields(lines[0]) {
		if header == "LIMIT" {
			limitColumn = index
		}
	}
	if limitColumn < 0 {
		t.Fatalf("table has no LIMIT column:\n%s", output.String())
	}
	for _, line := range lines[1:] {
		cells := strings.Fields(line)
		if got := cells[limitColumn]; got != want[cells[1]] {
			t.Errorf("Pod %s: LIMIT = %q, want %q", cells[1], got, want[cells[1]])
		}
	}
}

func TestTopContainerRowsCarryTheLimitState(t *testing.T) {
	rows := containerRows([]api.ContainerSnapshot{{
		Namespace: "default", PodName: "api", PodUID: "uid", ContainerName: "app", ContainerID: "id",
		Memory: model.MemoryBreakdown{MaxKnown: true, MaxUnlimited: true},
	}}, labels.Everything(), fields.Everything())
	if len(rows) != 1 || rows[0].LimitState != limitUnlimited {
		t.Fatalf("container rows = %+v, want one row with an unlimited limit", rows)
	}
}

func TestTopJSONCarriesTheLimitState(t *testing.T) {
	var output bytes.Buffer
	if err := writeTopRows(&output, limitTestRows(), topOptions{Output: "json"}, printPodRows); err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(output.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		name := row["name"].(string)
		if row["limitState"] != name && !(name == "unread" && row["limitState"] == "unknown") {
			t.Errorf("Pod %s: limitState = %v", name, row["limitState"])
		}
		_, hasBytes := row["limitBytes"]
		if hasBytes != (name == "finite") {
			t.Errorf("Pod %s: limitBytes present = %t, want it only for a finite limit", name, hasBytes)
		}
	}
}

func TestTopCSVCarriesTheLimitState(t *testing.T) {
	var output bytes.Buffer
	if err := writeTopRows(&output, limitTestRows(), topOptions{Output: "csv"}, printPodRows); err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(&output).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	header := records[0]
	if header[len(header)-1] != "limit_state" || header[7] != "limit_bytes" {
		t.Fatalf("CSV header = %v, want limit_bytes kept in place and limit_state added last", header)
	}
	want := map[string][2]string{"finite": {"67108864", "finite"}, "unlimited": {"0", "unlimited"}, "unread": {"0", "unknown"}}
	for _, record := range records[1:] {
		got := [2]string{record[7], record[len(record)-1]}
		if got != want[record[2]] {
			t.Errorf("Pod %s: limit_bytes, limit_state = %v, want %v", record[2], got, want[record[2]])
		}
	}
}

func TestTopRowsWithoutALimitColumnOmitTheLimitState(t *testing.T) {
	rows := namespaceRows([]api.NamespaceSnapshot{{Namespace: "default"}}, fields.Everything())
	body, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "limitState") {
		t.Fatalf("namespace rows carry a limit state: %s", body)
	}
}
