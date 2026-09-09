// The local fixture invokes the production reader using only a temporary
// tenant token and the API server's public address and CA material.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/kube"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	kubeconfig := flag.String("kubeconfig", "", "bootstrap configuration")
	tokenFile := flag.String("token-file", "", "short-lived caller token")
	namespace := flag.String("namespace", "kube-memlens-csi-e2e", "query namespace")
	pod := flag.String("pod", "persistent", "query Pod")
	expect := flag.String("expect", "", "expected fixture state")
	flag.Parse()
	if err := run(*kubeconfig, *tokenFile, *namespace, *pod, *expect); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(path, tokenFile, namespace, pod, expect string) error {
	bootstrap, err := clientcmd.BuildConfigFromFlags("", path)
	if err != nil {
		return errors.New("bootstrap configuration unavailable")
	}
	token, err := os.ReadFile(tokenFile)
	if err != nil {
		return errors.New("fixture token unavailable")
	}
	config := &rest.Config{Host: bootstrap.Host, BearerToken: strings.TrimSpace(string(token)), TLSClientConfig: rest.TLSClientConfig{CAData: bootstrap.CAData, CAFile: bootstrap.CAFile}}
	reader, err := kube.NewVolumeHealthSource(config, kube.VolumeHealthOptions{Namespace: namespace})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		r, readErr := reader.Query(ctx, pod)
		if matches(r, readErr, expect) {
			return json.NewEncoder(os.Stdout).Encode(struct {
				Outcome                    string               `json:"outcome"`
				Case                       string               `json:"case"`
				Summary                    volumehealth.Summary `json:"summary"`
				CredentialsRetained        bool                 `json:"credentialsRetained"`
				RuntimeIdentifiersIncluded bool                 `json:"runtimeIdentifiersIncluded"`
			}{Outcome: "passed", Case: expect, Summary: r.Summary()})
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("volume-health fixture %s did not meet expected state (summary: %v)", expect, r)
		case <-ticker.C:
		}
	}
}

func matches(r volumehealth.Report, err error, expect string) bool {
	if expect == "denied" {
		var e *kube.HealthReadError
		return errors.As(err, &e) && e.Reason == volumehealth.AccessDenied && r.Summary().Observations == 0
	}
	if err != nil {
		return false
	}
	rows := r.Interactive()
	bySource := map[volumehealth.Source]volumehealth.Observation{}
	for _, o := range rows {
		bySource[o.Source] = o
	}
	pod, controller, backend := bySource[volumehealth.PodSource], bySource[volumehealth.ControllerSource], bySource[volumehealth.BackendSource]
	switch expect {
	case "off":
		return len(rows) == 3 && pod.Availability == volumehealth.Unreported && controller.Availability == volumehealth.Unreported && backend.Availability == volumehealth.Unreported
	case "adverse":
		return len(rows) == 3 && hasCondition(pod, "Degraded") && controller.State == volumehealth.StateHealthy && hasCondition(backend, "StorageDegraded")
	case "recovered":
		return len(rows) == 3 && pod.State == volumehealth.StateHealthy && hasCondition(controller, "DataLoss") && backend.Availability == volumehealth.Unreported
	case "node-denied":
		return len(rows) == 3 && backend.Availability == volumehealth.Forbidden && controller.Availability == volumehealth.Reported
	default:
		return false
	}
}

func hasCondition(o volumehealth.Observation, status volumehealth.Status) bool {
	return o.Adverse && o.State == volumehealth.StateAdverse && len(o.Conditions) == 1 && o.Conditions[0].Status == status && o.ProbeFreshness == volumehealth.FreshnessUnknown
}
