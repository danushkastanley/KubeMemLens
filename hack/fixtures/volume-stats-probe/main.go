// Exercise the production bounded reader with only a namespace viewer token.
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

	"github.com/danushkastanley/kube-memlens/internal/client"
	"github.com/danushkastanley/kube-memlens/internal/volumehealth"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	path := flag.String("kubeconfig", "", "owned fixture configuration")
	token := flag.String("token-file", "", "short-lived viewer token file")
	flag.Parse()
	if err := run(*path, *token); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(path, tokenPath string) error {
	bootstrap, err := clientcmd.BuildConfigFromFlags("", path)
	if err != nil {
		return errors.New("fixture configuration unavailable")
	}
	token, err := os.ReadFile(tokenPath)
	if err != nil {
		return errors.New("fixture token unavailable")
	}
	config := &rest.Config{Host: bootstrap.Host, BearerToken: strings.TrimSpace(string(token)), TLSClientConfig: rest.TLSClientConfig{CAData: bootstrap.CAData, CAFile: bootstrap.CAFile}}
	scope, err := client.NamespaceScope("kube-memlens-csi-e2e")
	if err != nil {
		return err
	}
	reader, err := client.NewKubernetesAPIClient(config, scope, 8*time.Second)
	if err != nil {
		return errors.New("fixture reader unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	response, err := reader.PodVolumes(ctx, "kube-memlens-csi-e2e", "persistent", "")
	if err != nil {
		return errors.New("production volume reader failed")
	}
	if len(response.Context.Volumes) != 1 || response.Context.Volumes[0].Usage.Availability != volumehealth.Reported || response.Context.Volumes[0].Usage.Filesystem == nil {
		return errors.New("fixture measurement unavailable")
	}
	if _, err := reader.PodVolumes(ctx, "kube-memlens-csi-e2e", "persistent", string(response.UID)); err != nil {
		return errors.New("selected Pod binding failed")
	}
	if _, err := reader.PodVolumes(ctx, "kube-memlens-csi-e2e", "persistent", "retired-fixture-uid"); err == nil {
		return errors.New("replaced Pod binding accepted")
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]bool{"boundedRead": true, "selectedUIDChecked": true, "wrongUIDRejected": true})
}
