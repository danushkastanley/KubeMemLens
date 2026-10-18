// Command memory-history-prometheus exercises the production query adapter
// against an operator-verified, loopback-only Prometheus fixture.
package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	mh "github.com/danushkastanley/kube-memlens/internal/memoryhistory"
	"github.com/danushkastanley/kube-memlens/internal/promhistory"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() (result error) {
	binaryFlag := flag.String("prometheus-binary", "", "path to an independently verified local Prometheus binary")
	evidenceFlag := flag.String("evidence-dir", "", "existing private directory for a fresh fixture run")
	flag.Parse()
	if *binaryFlag == "" || *evidenceFlag == "" || flag.NArg() != 0 {
		return fmt.Errorf("set --prometheus-binary and --evidence-dir")
	}
	binary, err := filepath.Abs(*binaryFlag)
	if err != nil {
		return err
	}
	executable, err := os.Open(binary)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, hashErr := io.Copy(hash, executable)
	if err = errors.Join(hashErr, executable.Close()); err != nil {
		return err
	}
	binarySHA := fmt.Sprintf("%x", hash.Sum(nil))
	base, err := filepath.Abs(*evidenceFlag)
	if err != nil {
		return err
	}

	owned, err := os.MkdirTemp(base, "run-")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var value atomic.Int64
	value.Store(1048576)
	var duplicate atomic.Bool
	podLabels := `cluster="fixture",node="fixture-node",node_uid="node-uid",namespace="fixture",pod="fixture-pod",pod_uid="pod-uid",container="app",container_id="containerd://current",team="one",Team="two"`
	nodeLabels := `cluster="fixture",node="fixture-node",node_uid="node-uid",id="/",namespace="",pod="",container=""`
	metrics := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprintf(w, "container_memory_working_set_bytes{%s} %d\ncontainer_memory_rss{%s} 524288\n", podLabels, value.Load(), podLabels)
		fmt.Fprintf(w, "container_memory_working_set_bytes{%s} 4194304\ncontainer_memory_rss{%s} 2097152\n", nodeLabels, nodeLabels)
		fmt.Fprintf(w, "container_memory_working_set_bytes{%s} 9999999\n", strings.Replace(podLabels, `pod_uid="pod-uid"`, `pod_uid="other-uid"`, 1))
		if duplicate.Load() {
			fmt.Fprintf(w, "container_memory_working_set_bytes{%s,replica=\"duplicate\"} %d\n", podLabels, value.Load())
		}
	}))
	defer metrics.Close()
	ca, err := certificates(owned)
	if err != nil {
		return err
	}
	config := "global:\n  scrape_interval: 1s\n  scrape_timeout: 500ms\nscrape_configs:\n  - job_name: fixture\n    static_configs:\n      - targets: ['" + strings.TrimPrefix(metrics.URL, "http://") + "']\n"
	if err = os.WriteFile(filepath.Join(owned, "prometheus.yml"), []byte(config), 0600); err != nil {
		return err
	}
	web := "tls_server_config:\n  cert_file: '" + filepath.Join(owned, "server.pem") + "'\n  key_file: '" + filepath.Join(owned, "server-key.pem") + "'\n  min_version: TLS12\n"
	if err = os.WriteFile(filepath.Join(owned, "web.yml"), []byte(web), 0600); err != nil {
		return err
	}
	// Reserve an ephemeral loopback address, then hand it to the external binary.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	address := ln.Addr().String()
	ln.Close()
	log, err := os.OpenFile(filepath.Join(owned, "prometheus.log"), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.CommandContext(ctx, binary, "--config.file="+filepath.Join(owned, "prometheus.yml"), "--web.config.file="+filepath.Join(owned, "web.yml"), "--web.listen-address="+address, "--storage.tsdb.path="+filepath.Join(owned, "data"), "--storage.tsdb.retention.time=1h", "--storage.tsdb.retention.size=64MB", "--query.max-concurrency=1", "--query.timeout=5s", "--query.max-samples=100000")
	cmd.Env = append(os.Environ(), "GOMAXPROCS=1")
	cmd.Stdout = log
	cmd.Stderr = log
	if err = cmd.Start(); err != nil {
		return err
	}
	stopped := false
	defer func() {
		if !stopped {
			signalErr := cmd.Process.Signal(syscall.SIGTERM)
			result = errors.Join(result, signalErr, cmd.Wait())
		}
	}()
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(ca)
	probe := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}, Timeout: time.Second}
	defer probe.CloseIdleConnections()
	if err = poll(ctx, func() (bool, error) {
		req, _ := http.NewRequestWithContext(ctx, "GET", "https://"+address+"/-/ready", nil)
		res, e := probe.Do(req)
		if e != nil {
			return false, nil
		}
		defer res.Body.Close()
		io.Copy(io.Discard, res.Body)
		return res.StatusCode == 200, nil
	}); err != nil {
		return fmt.Errorf("readiness: %w", err)
	}
	client, err := promhistory.New(promhistory.Options{URL: "https://" + address, Cluster: "fixture", CAData: ca})
	if err != nil {
		return err
	}
	defer client.Close()
	now := time.Now().UTC()
	target := mh.Target{Namespace: "fixture", Pod: "fixture-pod", PodUID: "pod-uid", Container: "app", ContainerID: "containerd://current", Node: "fixture-node", NodeUID: "node-uid", PodCreatedAt: now.Add(-time.Hour), StartedAt: now.Add(-time.Hour)}
	selection := mh.Selection{Request: mh.Request{Scope: mh.Container, Namespace: "fixture", Name: "fixture-pod", Container: "app"}, UID: "pod-uid", ResolvedAt: now, Targets: []mh.Target{target}}
	var receipts []map[string]any
	check := func(name string, s mh.Selection, m mh.Metric, want uint64) error {
		var report mh.Report
		err := poll(ctx, func() (bool, error) {
			at := time.Now().UTC().Truncate(time.Second)
			q := mh.Query{Source: mh.Prometheus, Metric: m, Start: at, End: at, Step: time.Minute}
			r, e := client.Query(ctx, s, q)
			if e != nil {
				return false, e
			}
			report = r
			if len(r.Series) != 1 || len(r.Series[0].Points) != 1 {
				return false, fmt.Errorf("unexpected series shape")
			}
			p := r.Series[0].Points[0]
			return p.Bytes != nil && *p.Bytes == want && p.State == mh.Fresh, nil
		})
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		receipts = append(receipts, map[string]any{"case": name, "report": report})
		return nil
	}
	if err = check("working-set-and-original-sample-time", selection, mh.WorkingSet, 1048576); err != nil {
		return err
	}
	if err = check("rss", selection, mh.RSS, 524288); err != nil {
		return err
	}
	value.Store(262144)
	if err = check("gauge-decrease", selection, mh.WorkingSet, 262144); err != nil {
		return err
	}
	value.Store(0)
	if err = check("reported-zero", selection, mh.WorkingSet, 0); err != nil {
		return err
	}
	node := mh.Selection{Request: mh.Request{Scope: mh.Node, Name: "fixture-node"}, UID: "node-uid", ResolvedAt: now, Targets: []mh.Target{{Node: "fixture-node", NodeUID: "node-uid", StartedAt: now.Add(-time.Hour)}}}
	if err = check("node-root-working-set", node, mh.WorkingSet, 4194304); err != nil {
		return err
	}
	missing := selection
	missing.Targets = append([]mh.Target(nil), selection.Targets...)
	missing.Targets[0].ContainerID = "containerd://replaced"
	at := time.Now().UTC().Truncate(time.Second)
	q := mh.Query{Source: mh.Prometheus, Metric: mh.WorkingSet, Start: at, End: at, Step: time.Minute}
	r, err := client.Query(ctx, missing, q)
	if err != nil {
		return err
	}
	if r.State != mh.Missing || r.Series[0].Points[0].Bytes != nil {
		return fmt.Errorf("missing instance was not missing")
	}
	receipts = append(receipts, map[string]any{"case": "replacement-excludes-old-series", "report": r})
	duplicate.Store(true)
	if err = poll(ctx, func() (bool, error) {
		at := time.Now().UTC().Truncate(time.Second)
		q.Start = at
		q.End = at
		_, e := client.Query(ctx, selection, q)
		if errors.Is(e, mh.ErrSource) {
			return true, nil
		}
		return false, e
	}); err != nil {
		return fmt.Errorf("duplicate-series: %w", err)
	}
	receipts = append(receipts, map[string]any{"case": "duplicate-target-series-rejected", "error": mh.ErrSource.Error()})
	cmd.Process.Signal(syscall.SIGTERM)
	err = cmd.Wait()
	stopped = true
	if err != nil {
		return fmt.Errorf("Prometheus shutdown: %w", err)
	}
	receipt := map[string]any{"status": "passed", "binarySHA256": binarySHA, "platform": runtime.GOOS + "/" + runtime.GOARCH, "completedAt": time.Now().UTC(), "transport": "verified TLS loopback", "cases": receipts, "prometheusExited": true, "scope": "real PromQL evaluation with synthetic identities; not Linux or Kubernetes qualification"}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(owned, "result.json"), append(data, '\n'), 0600); err != nil {
		return err
	}
	fmt.Println(filepath.Join(owned, "result.json"))
	return nil
}
func poll(ctx context.Context, fn func() (bool, error)) error {
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		ok, err := fn()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("condition not met within 15s")
		case <-tick.C:
		}
	}
}
