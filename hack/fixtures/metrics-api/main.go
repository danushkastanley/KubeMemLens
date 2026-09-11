// Controlled Metrics API fixture for local source verification. Not shipped.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	certDir := flag.String("write-cert-dir", "", "write private local test certificates and exit")
	namespace := flag.String("namespace", "kube-memlens-metrics-e2e", "fixture namespace")
	cert := flag.String("tls-cert", "/tls/tls.crt", "serving certificate")
	key := flag.String("tls-key", "/tls/tls.key", "serving key")
	podUID := flag.String("pod-uid", "fixture-uid", "UID of the controlled Pod; empty means unreported")
	flag.Parse()
	if *certDir != "" {
		if err := writeCertificates(*certDir, *namespace); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	server := http.Server{Addr: ":9443", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { serve(w, r, *podUID) }), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	if err := server.ListenAndServeTLS(*cert, *key); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func serve(w http.ResponseWriter, r *http.Request, podUID string) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path == "/healthz" {
		w.WriteHeader(http.StatusOK)
		return
	}
	if strings.Contains(r.Header.Get("Accept"), "apidiscovery.k8s.io") {
		w.WriteHeader(http.StatusNotAcceptable)
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 || parts[0] != "apis" || parts[1] != "metrics.k8s.io" || (parts[2] != "v1" && parts[2] != "v1beta1") {
		http.NotFound(w, r)
		return
	}
	version := "metrics.k8s.io/" + parts[2]
	w.Header().Set("Content-Type", "application/json")
	if len(parts) == 3 {
		_ = json.NewEncoder(w).Encode(map[string]any{"apiVersion": "v1", "kind": "APIResourceList", "groupVersion": version, "resources": []any{map[string]any{"name": "pods", "kind": "PodMetrics", "namespaced": true, "verbs": []string{"get", "list"}}}})
		return
	}
	if len(parts) != 6 || parts[3] != "namespaces" || parts[5] != "pods" {
		http.NotFound(w, r)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"apiVersion": version, "kind": "PodMetricsList", "items": []any{map[string]any{
		"metadata":  map[string]string{"namespace": parts[4], "name": "fixture-pod", "uid": podUID},
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano), "window": "15s",
		"containers": []any{map[string]any{"name": "worker", "usage": map[string]string{"cpu": "125m", "memory": "32Mi"}}},
	}}})
}
