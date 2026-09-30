//go:build linux || darwin

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/trace"
	"github.com/danushkastanley/kube-memlens/prototype/trace/admissionkube"
	"github.com/danushkastanley/kube-memlens/prototype/trace/filecache"
	"github.com/danushkastanley/kube-memlens/prototype/trace/installcheck"
	"github.com/danushkastanley/kube-memlens/prototype/trace/workerinstall"
	"k8s.io/client-go/rest"
)

func runInstallCheck(ctx context.Context, args []string, out, errOut io.Writer) error {
	flags := flag.NewFlagSet("_install-check", flag.ContinueOnError)
	flags.SetOutput(errOut)
	specFile := flags.String("spec", "/installation/check.json", "bounded installation specification")
	policyFile := flags.String("acceptance-policy", "/acceptance/policy.json", "pinned installation policy")
	apiTrust := flags.String("api-trust", "/tls", "mounted admission API trust directory")
	nodeTrust := flags.String("node-trust", "/node-trust", "mounted public node certificates")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		return installcheck.ErrConfiguration
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	data, err := readInstallationFile(*specFile, installcheck.MaxSpecBytes)
	if err != nil {
		return installcheck.ErrConfiguration
	}
	spec, err := installcheck.Decode(data)
	if err != nil {
		return err
	}
	policy, err := workerinstall.ReadPinnedFile(*policyFile, spec.PolicySHA256)
	if err != nil {
		return installcheck.ErrTrust
	}
	for _, node := range spec.Nodes {
		if _, err := policy.WorkerSHA256(node.Architecture); err != nil {
			return installcheck.ErrTrust
		}
		for _, kind := range []trace.Kind{trace.Files, trace.Cache, trace.OOM} {
			if _, err := policy.ManifestSHA256(filecache.ArtifactID{Kind: kind, Architecture: node.Architecture}); err != nil {
				return installcheck.ErrTrust
			}
		}
	}
	material, err := readInstallTrust(spec, *apiTrust, *nodeTrust)
	defer clear(material.APIKey)
	defer clear(material.ControlKey)
	if err != nil || installcheck.CheckTrust(spec, material, time.Now()) != nil {
		return installcheck.ErrTrust
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		return installcheck.ErrUnavailable
	}
	client, err := admissionkube.NewClient(config)
	if err != nil {
		return installcheck.ErrUnavailable
	}
	if err := installcheck.CheckNodes(ctx, client.CoreV1().Nodes(), spec); err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, `{"schemaVersion":1,"nodeMetadataAndTrust":"verified","hostPreflight":"pending","resourceQualified":false}`)
	return err
}

func readInstallTrust(spec installcheck.Spec, apiDirectory, nodeDirectory string) (installcheck.TrustMaterial, error) {
	material := installcheck.TrustMaterial{Nodes: map[string]installcheck.NodeTrust{}}
	files := []struct {
		name   string
		target *[]byte
	}{{"tls.crt", &material.APICertificate}, {"tls.key", &material.APIKey},
		{"node-client.crt", &material.ControlCertificate}, {"node-client.key", &material.ControlKey},
		{"node-ca.crt", &material.NodeCA}}
	for _, file := range files {
		data, err := readInstallationFile(filepath.Join(apiDirectory, file.name), 64<<10)
		if err != nil {
			return material, installcheck.ErrTrust
		}
		*file.target = data
	}
	for _, node := range spec.Nodes {
		certificate, err := readInstallationFile(filepath.Join(nodeDirectory, node.ID, "tls.crt"), 64<<10)
		if err != nil {
			return material, installcheck.ErrTrust
		}
		ca, err := readInstallationFile(filepath.Join(nodeDirectory, node.ID, "control-ca.crt"), 64<<10)
		if err != nil {
			return material, installcheck.ErrTrust
		}
		material.Nodes[node.ID] = installcheck.NodeTrust{Certificate: certificate, ControlCA: ca}
	}
	return material, nil
}
