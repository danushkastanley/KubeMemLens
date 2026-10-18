package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type historyRoundTrip func(*http.Request) (*http.Response, error)

func (f historyRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHistoryDeadlineIsIndependentFromLiveReads(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		report, request, q := clientHistoryFixture(t)
		body, _ := json.Marshal(struct {
			metav1.TypeMeta   `json:",inline"`
			metav1.ObjectMeta `json:"metadata"`
			History           any `json:"history"`
		}{metav1.TypeMeta{APIVersion: "memory.kubememlens.io/v1alpha1", Kind: "MemoryHistory"}, metav1.ObjectMeta{Name: "api", Namespace: "team-a", UID: "pod-uid"}, report})
		transport := historyRoundTrip(func(r *http.Request) (*http.Response, error) {
			select {
			case <-time.After(6 * time.Second):
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		})
		scope, _ := NamespaceScope("team-a")
		c := &KubernetesAPIClient{baseURL: "https://fixture.invalid/apis/memory.kubememlens.io/v1alpha1", scope: scope, httpClient: &http.Client{Transport: transport, Timeout: defaultTimeout}}
		if _, err := c.MemoryHistory(t.Context(), request, q); err != nil {
			t.Fatal("history was cut off by the live-read timeout", err)
		}
		if c.httpClient.Timeout != defaultTimeout {
			t.Fatal("history changed shared live-read timeout")
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		if _, err := c.MemoryHistory(ctx, request, q); err == nil {
			t.Fatal("earlier caller deadline ignored")
		}
	})
}
