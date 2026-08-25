package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation"
)

func parseExpectedNodeSelector(raw string) (string, error) {
	var values map[string]string
	decoder := json.NewDecoder(strings.NewReader(raw))
	if err := decoder.Decode(&values); err != nil {
		return "", fmt.Errorf("decode expected Node selector: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return "", fmt.Errorf("decode expected Node selector: unexpected trailing JSON")
	}
	selector := labels.SelectorFromSet(values)
	if _, err := labels.Parse(selector.String()); err != nil {
		return "", fmt.Errorf("validate expected Node selector: %w", err)
	}
	return selector.String(), nil
}

func parseExpectedNodeTolerations(raw string) ([]corev1.Toleration, error) {
	var values []corev1.Toleration
	decoder := json.NewDecoder(strings.NewReader(raw))
	if err := decoder.Decode(&values); err != nil {
		return nil, fmt.Errorf("decode expected Node tolerations: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("decode expected Node tolerations: unexpected trailing JSON")
	}
	for _, item := range values {
		operator := item.Operator
		if operator == "" {
			operator = corev1.TolerationOpEqual
		}
		if operator != corev1.TolerationOpEqual && operator != corev1.TolerationOpExists {
			return nil, fmt.Errorf("validate expected Node tolerations: unsupported operator %q", item.Operator)
		}
		if item.Key != "" && len(validation.IsQualifiedName(item.Key)) > 0 {
			return nil, fmt.Errorf("validate expected Node tolerations: invalid key")
		}
		if operator == corev1.TolerationOpEqual && (item.Key == "" || len(validation.IsValidLabelValue(item.Value)) > 0) {
			return nil, fmt.Errorf("validate expected Node tolerations: invalid Equal match")
		}
		if operator == corev1.TolerationOpExists && item.Value != "" {
			return nil, fmt.Errorf("validate expected Node tolerations: Exists requires an empty value")
		}
		switch item.Effect {
		case "", corev1.TaintEffectNoSchedule, corev1.TaintEffectPreferNoSchedule, corev1.TaintEffectNoExecute:
		default:
			return nil, fmt.Errorf("validate expected Node tolerations: unsupported effect %q", item.Effect)
		}
	}
	return values, nil
}
