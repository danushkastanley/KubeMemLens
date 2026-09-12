// Verifies the production query with a short-lived namespace identity on kind.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/capability"
	"github.com/danushkastanley/kube-memlens/internal/client"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	config := flag.String("kubeconfig", "", "private namespace-reader kubeconfig")
	namespace := flag.String("namespace", "", "authorised namespace")
	denied := flag.String("denied-namespace", "", "separate namespace without access")
	expect := flag.String("expect", "measured", "measured, missing or revoked")
	artifact := flag.String("artifact", "", "sanitised result path")
	flag.Parse()
	scope, err := client.NamespaceScope(*namespace)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	opts := client.Options{Kubeconfig: *config, ReadScope: scope, EvidenceMode: capability.Restricted, Timeout: 10 * time.Second}
	session, err := client.NewEvidenceSession(ctx, opts)
	if *expect == "revoked" {
		if !deniedError(err) {
			return fmt.Errorf("revoked caller retained source access")
		}
		return save(*artifact, *expect)
	}
	if err != nil {
		return fmt.Errorf("source selection failed: %w", err)
	}
	batch, err := session.Observations.Current(ctx)
	if err != nil {
		return fmt.Errorf("current query failed: %w", err)
	}
	if len(batch.Pods) != 1 || batch.Pods[0].Namespace != *namespace || batch.Pods[0].Name != "fixture-pod" || batch.Pods[0].Context.CreatedAt.IsZero() || len(batch.Pods[0].Containers) != 2 {
		return fmt.Errorf("Pod inventory or age did not match")
	}
	pod := batch.Pods[0]
	if pod.Cgroup != nil || pod.Containers[1].WorkingSet.Bytes != nil || batch.Completeness != capability.Partial {
		return fmt.Errorf("missing or deep evidence was fabricated")
	}
	switch *expect {
	case "measured":
		value := pod.Containers[0].WorkingSet
		if value.Bytes == nil || *value.Bytes != 32<<20 || value.Evidence.Source != capability.KubernetesMetrics || value.Evidence.CapturedAt.IsZero() || value.Evidence.Window != 15*time.Second || value.Reason != "" {
			return fmt.Errorf("working-set source, time or UID join failed")
		}
		if pod.WorkingSet.Coverage.Reported != 1 || pod.WorkingSet.Coverage.Expected != 2 {
			return fmt.Errorf("missing container coverage lost")
		}
	case "missing":
		if pod.WorkingSet.Bytes != nil {
			return fmt.Errorf("missing metrics became measured memory")
		}
	default:
		return fmt.Errorf("invalid expected outcome")
	}
	other, err := client.NamespaceScope(*denied)
	if err != nil {
		return err
	}
	opts.ReadScope = other
	if _, err = client.NewEvidenceSession(ctx, opts); !deniedError(err) {
		return fmt.Errorf("cross-namespace discovery was permitted")
	}
	opts.ReadScope = client.AllNamespacesScope()
	if _, err = client.NewEvidenceSession(ctx, opts); !deniedError(err) {
		return fmt.Errorf("implicit cluster access was permitted")
	}
	// Auto must use the same authorised restricted source when deep is denied.
	opts.ReadScope = scope
	opts.EvidenceMode = capability.Auto
	auto, err := client.NewEvidenceSession(ctx, opts)
	if err != nil || auto.Plan.Mode != capability.Restricted {
		return fmt.Errorf("auto did not select the authorised restricted source")
	}
	return save(*artifact, *expect)
}

func deniedError(err error) bool {
	var failure *capability.SelectionError
	return errors.As(err, &failure) && failure.Reason == capability.AccessDenied
}

func save(path, outcome string) error {
	data, err := json.MarshalIndent(map[string]any{"outcome": "passed", "scenario": outcome, "runtimeIdentifiersIncluded": false, "credentialsRetained": false}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
		return err
	}
	fmt.Printf("agentless %s query verified\n", outcome)
	return nil
}
