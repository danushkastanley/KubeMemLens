// Command memory-history-source is a synthetic, TLS-protected range API fixture.
// It is used only to exercise Kubernetes permission and provider-failure paths.
package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

type series struct {
	Labels     map[string]string `json:"labels"`
	Started    int64             `json:"started"`
	WorkingSet uint64            `json:"workingSet"`
	RSS        uint64            `json:"rss"`
}
type configuration struct {
	Mode     string   `json:"mode"`
	Series   []series `json:"series"`
	revision string
}
type fixture struct {
	path      string
	token     string
	requests  atomic.Uint64
	active    atomic.Int64
	cancelled atomic.Uint64
}

func main() {
	config := flag.String("config", "", "mounted synthetic series configuration")
	cert := flag.String("cert", "", "fixture serving certificate")
	key := flag.String("key", "", "fixture private key")
	tokenFile := flag.String("token", "", "fixture-only bearer credential")
	listen := flag.String("listen", ":9443", "fixture listen address; use loopback for host checks")
	flag.Parse()
	token, err := os.ReadFile(*tokenFile)
	if err != nil || len(strings.TrimSpace(string(token))) == 0 {
		log.Fatal("fixture token unavailable")
	}
	f := &fixture{path: *config, token: strings.TrimSpace(string(token))}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/query_range", f.query)
	mux.HandleFunc("/metrics", f.metrics)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		config, err := f.configuration()
		if err != nil {
			http.Error(w, "fixture configuration unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"mode": config.Mode, "revision": config.revision, "requests": f.requests.Load(), "active": f.active.Load(), "cancelled": f.cancelled.Load()})
	})
	server := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 16 << 10, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	if err := server.ListenAndServeTLS(*cert, *key); err != nil && err != http.ErrServerClosed {
		log.Fatal("fixture serving failed")
	}
}
func (f *fixture) query(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+f.token)) != 1 {
		http.Error(w, "denied", 403)
		return
	}
	f.requests.Add(1)
	f.active.Add(1)
	defer f.active.Add(-1)
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	if r.ParseForm() != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	config, err := f.configuration()
	if err != nil {
		http.Error(w, "invalid fixture configuration", 503)
		return
	}
	switch config.Mode {
	case "normal", "duplicate", "wrong-identity", "malformed", "stale", "gaps":
	case "unavailable":
		http.Error(w, "controlled provider failure", 503)
		return
	case "slow":
		timer := time.NewTimer(8 * time.Second)
		defer timer.Stop()
		select {
		case <-r.Context().Done():
			f.cancelled.Add(1)
			return
		case <-timer.C:
		}
	case "delay":
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		select {
		case <-r.Context().Done():
			f.cancelled.Add(1)
			return
		case <-timer.C:
		}
	default:
		http.Error(w, "unknown fixture mode", 503)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if config.Mode == "malformed" {
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":null}}`))
		return
	}
	start, e1 := strconv.ParseInt(r.Form.Get("start"), 10, 64)
	end, e2 := strconv.ParseInt(r.Form.Get("end"), 10, 64)
	step, e3 := strconv.ParseInt(r.Form.Get("step"), 10, 64)
	if e1 != nil || e2 != nil || e3 != nil || step < 60 || end < start || end-start > 7*24*3600 || (end-start)/step > 240 {
		http.Error(w, "fixture range exceeded", 400)
		return
	}
	expression := r.Form.Get("query")
	metric := "container_memory_working_set_bytes"
	if strings.Contains(expression, "container_memory_rss{") {
		metric = "container_memory_rss"
	}
	result := make([]map[string]any, 0)
	for _, s := range config.Series {
		matches := true
		for k, v := range s.Labels {
			if !strings.Contains(expression, k+"="+strconv.Quote(v)) {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		values, sampled := make([][2]any, 0), make([][2]any, 0)
		for at := start; at <= end; at += step {
			stamp := at - 1
			if config.Mode == "stale" {
				stamp = at - 130
			}
			if stamp < s.Started {
				continue
			}
			if config.Mode == "gaps" && (at-start)/step%2 == 1 {
				continue
			}
			value := s.WorkingSet
			if metric == "container_memory_rss" {
				value = s.RSS
			}
			values = append(values, [2]any{at, fmt.Sprint(value)})
			sampled = append(sampled, [2]any{at, fmt.Sprint(stamp)})
		}
		if len(values) == 0 {
			continue
		}
		for field, points := range map[string][][2]any{"value": values, "sampled": sampled} {
			labels := make(map[string]string, len(s.Labels)+2)
			for k, v := range s.Labels {
				labels[k] = v
			}
			if config.Mode == "wrong-identity" {
				labels["node_uid"] = "wrong-node-uid"
			}
			labels["kml_history_field"] = field
			if field == "value" {
				labels["__name__"] = metric
			}
			row := map[string]any{"metric": labels, "values": points}
			result = append(result, row)
			if config.Mode == "duplicate" {
				result = append(result, row)
			}
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": result}})
}

func (f *fixture) configuration() (configuration, error) {
	file, err := os.Open(f.path)
	if err != nil {
		return configuration{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 65537))
	var config configuration
	if err != nil || len(data) > 65536 || json.Unmarshal(data, &config) != nil || len(config.Series) > 32 {
		return configuration{}, fmt.Errorf("invalid fixture configuration")
	}
	config.revision = fmt.Sprintf("%x", sha256.Sum256(data))
	return config, nil
}
