package cli

import (
	"fmt"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/kube"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	minRequestTimeout = time.Second
	maxRequestTimeout = 5 * time.Minute
)

func (f *collectorFlags) checkRequestTimeout() error {
	if f.requestTimeout < minRequestTimeout || f.requestTimeout > maxRequestTimeout {
		return fmt.Errorf("--request-timeout must be between %s and %s, got %s", minRequestTimeout, maxRequestTimeout, f.requestTimeout)
	}
	return nil
}

// applyContextNamespace gives a command's --namespace the namespace of the
// kubeconfig context when the flag was not set, as kubectl plugins are
// expected to. A kubeconfig that cannot be read leaves "default": the command
// reports the kubeconfig problem itself when it connects.
func (f *collectorFlags) applyContextNamespace(cmd *cobra.Command) {
	flag := cmd.Flags().Lookup("namespace")
	if flag == nil || flag.Changed || flag.Annotations[contextNamespaceAnnotation] == nil {
		return
	}
	namespace, err := kube.ContextNamespace(f.kubeconfig, f.context)
	if err != nil {
		return
	}
	// The flag accepts any string, so this cannot fail.
	_ = flag.Value.Set(namespace)
}

// contextNamespaceAnnotation marks a --namespace flag whose fallback is
// "default". Some commands register the flag with an empty default that means
// "no namespace given"; those are left alone.
const contextNamespaceAnnotation = "kubememlens.io/context-namespace"

// describeContextNamespace marks those flags and makes --help say where the
// namespace comes from.
func describeContextNamespace(root *cobra.Command) {
	describe := func(flag *pflag.Flag) {
		if flag.Name == "namespace" && flag.DefValue == "default" {
			flag.Annotations = map[string][]string{contextNamespaceAnnotation: {"true"}}
			flag.DefValue = ""
			flag.Usage += "; defaults to the namespace of the kubeconfig context"
		}
	}
	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		cmd.Flags().VisitAll(describe)
		cmd.PersistentFlags().VisitAll(describe)
		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	visit(root)
}
