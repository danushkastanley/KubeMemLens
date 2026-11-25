package kube

import (
	"fmt"
	"strings"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func BuildConfig(kubeconfigPath string, contextName string) (*rest.Config, error) {
	kubeconfigPath = strings.TrimSpace(kubeconfigPath)
	contextName = strings.TrimSpace(contextName)
	if kubeconfigPath != "" || contextName != "" {
		return buildKubeconfig(kubeconfigPath, contextName)
	}

	inClusterConfig, inClusterErr := rest.InClusterConfig()
	if inClusterErr == nil {
		return inClusterConfig, nil
	}

	kubeconfig, kubeconfigErr := buildKubeconfig("", "")
	if kubeconfigErr != nil {
		return nil, fmt.Errorf("build Kubernetes config: in-cluster config failed: %v; kubeconfig failed: %w", inClusterErr, kubeconfigErr)
	}
	return kubeconfig, nil
}

func buildKubeconfig(kubeconfigPath string, contextName string) (*rest.Config, error) {
	config, err := clientConfig(kubeconfigPath, contextName).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("build kubeconfig: %w", err)
	}
	return config, nil
}

// ContextNamespace returns the namespace of the selected kubeconfig context,
// as kubectl would use it: "default" when the context names none, and the
// Pod's own namespace when running in a cluster without a kubeconfig.
func ContextNamespace(kubeconfigPath string, contextName string) (string, error) {
	namespace, _, err := clientConfig(strings.TrimSpace(kubeconfigPath), strings.TrimSpace(contextName)).Namespace()
	if err != nil {
		return "", fmt.Errorf("read kubeconfig namespace: %w", err)
	}
	return namespace, nil
}

func clientConfig(kubeconfigPath string, contextName string) clientcmd.ClientConfig {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfigPath != "" {
		loadingRules.ExplicitPath = kubeconfigPath
	}

	overrides := &clientcmd.ConfigOverrides{}
	if contextName != "" {
		overrides.CurrentContext = contextName
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)
}
