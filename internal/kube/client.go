package kube

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func NewClient() (kubernetes.Interface, error) {
	config, err := BuildConfig("", "")
	if err != nil {
		return nil, err
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}
	return client, nil
}

func ListPodsForNode(ctx context.Context, client kubernetes.Interface, nodeName string) ([]corev1.Pod, error) {
	options := metav1.ListOptions{}
	if nodeName != "" {
		options.FieldSelector = "spec.nodeName=" + nodeName
	}

	pods, err := client.CoreV1().Pods("").List(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}
	return pods.Items, nil
}
