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
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfigPath != "" {
		loadingRules.ExplicitPath = kubeconfigPath
	}

	overrides := &clientcmd.ConfigOverrides{}
	if contextName != "" {
		overrides.CurrentContext = contextName
	}

	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("build kubeconfig: %w", err)
	}
	return config, nil
}
