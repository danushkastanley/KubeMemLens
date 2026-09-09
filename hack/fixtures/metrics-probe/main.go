// Exercises the production source against a controlled local aggregated API.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/kube"
	"github.com/danushkastanley/kube-memlens/internal/resourcemetrics"
	"k8s.io/client-go/rest"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	configPath := flag.String("kubeconfig", "", "local cluster configuration")
	contextName := flag.String("context", "", "kind context")
	namespace := flag.String("namespace", "", "namespace to read")
	tokenFile := flag.String("token-file", "", "private short-lived caller token file")
	state := flag.String("state", "available", "expected source state")
	version := flag.String("version", "", "expected source version")
	artifact := flag.String("artifact", "", "sanitised result file")
	flag.Parse()
	if !strings.HasPrefix(*contextName, "kind-") {
		return fmt.Errorf("probe requires a kind context")
	}
	base, err := kube.BuildConfig(*configPath, *contextName)
	if err != nil {
		return err
	}
	token, err := os.ReadFile(*tokenFile)
	if err != nil {
		return err
	}
	// Copy only server trust, never the bootstrap administrator's client identity.
	config := &rest.Config{Host: base.Host, TLSClientConfig: rest.TLSClientConfig{CAData: base.CAData, CAFile: base.CAFile, ServerName: base.ServerName}, BearerToken: strings.TrimSpace(string(token))}
	source, err := resourcemetrics.New(config, resourcemetrics.Options{Namespace: *namespace})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var report resourcemetrics.Report
	for {
		report, err = source.Read(ctx)
		if err == nil && string(report.Availability) == *state && (*version == "" || report.APIVersion == "metrics.k8s.io/"+*version) {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("expected %s/%s; got %s/%s (%s)", *state, *version, report.Availability, report.APIVersion, report.Reason)
		case <-time.After(time.Second):
		}
	}
	if *state == "available" {
		if len(report.Observations) != 1 {
			return fmt.Errorf("expected one fixture observation")
		}
		value := report.Observations[0]
		if value.Identity.Namespace != *namespace || value.Identity.PodName != "fixture-pod" || value.Identity.PodUID != "fixture-uid" || value.Identity.ContainerName != "worker" || value.MemoryWorkingSetBytes != 32<<20 || value.CPUUsageNanocores != 125000000 || value.Window != 15*time.Second || value.Freshness != resourcemetrics.Fresh {
			return fmt.Errorf("source conversion or identity contract failed")
		}
	} else if len(report.Observations) != 0 {
		return fmt.Errorf("unavailable/denied source returned observations")
	}
	evidence := map[string]any{"outcome": "passed", "availability": report.Availability, "apiVersion": report.APIVersion, "observationCount": len(report.Observations), "runtimeIdentifiersIncluded": false, "credentialsRetained": false}
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(*artifact, append(data, '\n'), 0o600); err != nil {
		return err
	}
	fmt.Printf("resource metrics %s/%s verified\n", report.Availability, report.APIVersion)
	return nil
}
