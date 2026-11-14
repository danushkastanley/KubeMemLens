package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

// Every command that offers --output must accept -o as well.
func TestOutputFlagHasItsShorthandEverywhere(t *testing.T) {
	root := NewRootCommand(&bytes.Buffer{}, &bytes.Buffer{})
	for _, path := range [][]string{
		{"status"}, {"doctor"}, {"top", "pods"}, {"top", "containers"}, {"top", "workloads"}, {"top", "namespaces"},
		{"explain", "pod"}, {"explain", "workload"},
	} {
		t.Run(strings.Join(path, " "), func(t *testing.T) {
			command, _, err := root.Find(path)
			if err != nil {
				t.Fatal(err)
			}
			if command.Flags().Lookup("output") == nil {
				t.Fatal("command has no --output flag")
			}
			if command.Flags().ShorthandLookup("o") == nil {
				t.Fatal("--output has no -o shorthand")
			}
		})
	}
}

// The column has been labelled ANON since v1.0.3; rss stays as the older name.
func TestSortByAcceptsAnonAndKeepsRSS(t *testing.T) {
	for _, field := range []string{"anon", "rss"} {
		t.Run(field, func(t *testing.T) {
			opts := topOptions{Output: "table", SortBy: field, NoHeaders: true, WatchInterval: 2 * time.Second}
			if err := opts.validate(); err != nil {
				t.Fatalf("--sort-by %s was rejected: %v", field, err)
			}
			rows := []topRow{
				{Name: "large-total", TotalBytes: 9, AnonBytes: 1},
				{Name: "large-anon", TotalBytes: 2, AnonBytes: 8},
			}
			var output bytes.Buffer
			if err := writeTopRows(&output, rows, opts, printPodRows); err != nil {
				t.Fatal(err)
			}
			if strings.Index(output.String(), "large-anon") > strings.Index(output.String(), "large-total") {
				t.Fatalf("--sort-by %s did not sort by anonymous memory:\n%s", field, output.String())
			}
		})
	}
	if err := (topOptions{Output: "table", SortBy: "nonsense", WatchInterval: 2 * time.Second}).validate(); err == nil {
		t.Fatal("an unknown sort field was accepted")
	}
}

func TestExplainTablesLabelAnonymousMemoryAsAnon(t *testing.T) {
	pod := api.PodSnapshot{
		Namespace: "default", PodName: "api", NodeName: "node-a", Memory: model.MemoryBreakdown{TotalBytes: 4 << 20},
		Containers: []api.ContainerSnapshot{{Namespace: "default", PodName: "api", ContainerName: "app"}},
	}
	tables := map[string]func(*bytes.Buffer){
		"explain pod": func(w *bytes.Buffer) { printPodExplanation(w, pod, liveClock()) },
		"explain workload": func(w *bytes.Buffer) {
			printWorkloadExplanation(w, api.WorkloadSnapshot{Namespace: "default", Kind: "Deployment", Name: "api", PodCount: 1, Pods: []api.PodSnapshot{pod}})
		},
	}
	for name, print := range tables {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			print(&output)
			header := ""
			for _, line := range strings.Split(output.String(), "\n") {
				if strings.Contains(line, "TOTAL") && strings.Contains(line, "DIAGNOSIS") {
					header = line
				}
			}
			columns := strings.Fields(header)
			if !contains(columns, "ANON") || contains(columns, "RSS") {
				t.Fatalf("table header = %q, want an ANON column and no RSS column", header)
			}
		})
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
