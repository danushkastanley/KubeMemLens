// The local qualification client exercises production session validation without
// retaining raw frames. It is not part of the product's release binaries.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/kube"
	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/internal/traceclient"
	"github.com/danushkastanley/kube-memlens/internal/tracereport"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func main() {
	flags := flag.NewFlagSet("isolation-client", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	path := flags.String("config", "", "private fixture configuration")
	if flags.Parse(os.Args[1:]) != nil || flags.NArg() != 0 {
		fail()
	}
	c, err := readConfiguration(*path)
	if err != nil {
		fail()
	}
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(signalCtx, 60*time.Second)
	defer cancel()
	if run(ctx, c, os.Stdout, os.Stderr) != nil {
		fail()
	}
}
func fail() { _, _ = fmt.Fprintln(os.Stderr, "local isolation qualification failed"); os.Exit(1) }

func run(ctx context.Context, c configuration, output, progress io.Writer) (resultErr error) {
	config, err := kube.BuildConfig(c.Kubeconfig, c.Context)
	if err != nil || localCredentials(config) != nil {
		return errQualification
	}
	core, err := kubernetes.NewForConfig(config)
	if err != nil {
		return errQualification
	}
	lookup, stop := context.WithTimeout(ctx, 5*time.Second)
	pod, err := core.CoreV1().Pods(c.Namespace).Get(lookup, c.Pod, metav1.GetOptions{})
	stop()
	if err != nil || string(pod.UID) != c.PodUID || pod.Labels[fixtureLabel] != c.RunID {
		return errQualification
	}
	client, err := traceclient.New(config)
	if err != nil {
		return errQualification
	}
	defer client.Close()
	if client.Discover(ctx) != nil {
		return errQualification
	}
	selected, err := client.SelectPinned(ctx, c.Namespace, c.Pod, c.Container, traceclient.SelectionPin{PodUID: c.PodUID, ContainerID: c.ContainerID, NodeName: c.NodeName})
	if err != nil || !selected.ContainerStartedAt.Equal(c.ContainerStartedAt) {
		return errQualification
	}
	intent := traceclient.DefaultIntent(c.Kind)
	intent.Bounds.Duration = time.Duration(c.DurationSeconds) * time.Second
	intent.Bounds.Events = c.MaxEvents
	intent.Bounds.PathBytes = 64
	if c.ConfirmedPaths {
		intent.Paths = trace.ConfirmedPaths
	}
	session, err := traceclient.NewSession(client, selected, intent)
	if err != nil || session.Prepare(ctx) != nil || session.Start(ctx) != nil {
		return errQualification
	}
	defer func() {
		session.Cancel()
		select {
		case <-session.Done():
		case <-time.After(8 * time.Second):
			resultErr = errQualification
		}
	}()
	announcedID, announcedMetadata := false, false
	for {
		select {
		case <-session.Updates():
			snapshot := session.Snapshot()
			// The private orchestration channel needs the owned handle for adversarial
			// probes. It contains neither target names nor raw trace events.
			if !announcedID && snapshot.AdmissionID != "" {
				if json.NewEncoder(progress).Encode(map[string]string{"type": "admitted", "id": snapshot.AdmissionID}) != nil {
					return errQualification
				}
				announcedID = true
			}
			if !announcedMetadata && snapshot.Result.Metadata.SessionID != "" {
				if json.NewEncoder(progress).Encode(map[string]string{"type": "metadata"}) != nil {
					return errQualification
				}
				announcedMetadata = true
			}
		case <-session.Done():
			return report(session.Snapshot(), output)
		case <-ctx.Done():
			session.Cancel()
			select {
			case <-session.Done():
				return report(session.Snapshot(), output)
			case <-time.After(8 * time.Second):
				return errQualification
			}
		}
	}
}

func report(snapshot traceclient.Snapshot, output io.Writer) error {
	document, err := tracereport.New(snapshot, "isolation-qualification", time.Now().UTC())
	if err != nil || json.NewEncoder(output).Encode(document) != nil {
		return errQualification
	}
	if snapshot.State == traceclient.StateFailed || snapshot.Cleanup != traceclient.CleanupConfirmed {
		return errQualification
	}
	return nil
}
