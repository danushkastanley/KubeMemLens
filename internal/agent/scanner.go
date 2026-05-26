package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/cgroup"
	"github.com/danushkastanley/kube-memlens/internal/kube"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

type Scanner struct {
	CgroupRoot string
	NodeName   string
	Kube       bool
}

type ScanResult struct {
	Snapshot        api.AgentSnapshot
	ContainersFound int
	Mapped          int
	Unmapped        int
	TotalMemory     model.MemoryBreakdown
	RootFallback    bool
	WalkError       error
}

func (s Scanner) ScanOnce(name string) (model.MemoryBreakdown, error) {
	if s.CgroupRoot == "" {
		return model.MemoryBreakdown{}, fmt.Errorf("cgroup root is required")
	}

	if !LooksLikeCgroupDir(s.CgroupRoot) {
		return model.MemoryBreakdown{}, fmt.Errorf("%s does not look like a cgroup v2 memory directory", s.CgroupRoot)
	}

	return cgroup.ParseDirectory(name, s.CgroupRoot)
}

func (s Scanner) Scan(ctx context.Context, idx kube.PodIndex) (ScanResult, error) {
	if strings.TrimSpace(s.CgroupRoot) == "" {
		return ScanResult{}, fmt.Errorf("cgroup root is required")
	}

	capturedAt := time.Now().UTC()
	entries, walkErr := (cgroup.Walker{Root: s.CgroupRoot}).Walk()
	if len(entries) == 0 {
		breakdown, err := s.ScanOnce("local")
		if err != nil {
			if walkErr != nil {
				return ScanResult{}, fmt.Errorf("walk cgroups: %w", walkErr)
			}
			return ScanResult{}, err
		}

		return ScanResult{
			Snapshot: api.AgentSnapshot{
				NodeName:   s.NodeName,
				CapturedAt: capturedAt,
			},
			TotalMemory:  breakdown,
			RootFallback: true,
			WalkError:    walkErr,
		}, nil
	}

	snapshots := make([]api.ContainerSnapshot, 0, len(entries))
	memories := make([]model.MemoryBreakdown, 0, len(entries))
	mapped := 0
	unmapped := 0
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return ScanResult{}, ctx.Err()
		default:
		}

		memory := entry.Memory
		ref, ok := idx.Lookup(entry.ContainerID, entry.PodUID)
		if ok {
			mapped++
			memory.Name = ref.Namespace + "/" + ref.PodName + "/" + ref.ContainerName
			snapshots = append(snapshots, api.ContainerSnapshot{
				Namespace:     ref.Namespace,
				PodName:       ref.PodName,
				PodUID:        ref.PodUID,
				ContainerName: ref.ContainerName,
				ContainerID:   entry.ContainerID,
				NodeName:      chooseString(ref.NodeName, s.NodeName),
				CgroupPath:    entry.RelativePath,
				CapturedAt:    capturedAt,
				Memory:        memory,
			})
		} else {
			unmapped++
			memory.Name = entry.ContainerID
			snapshots = append(snapshots, api.ContainerSnapshot{
				ContainerID: entry.ContainerID,
				NodeName:    s.NodeName,
				CgroupPath:  entry.RelativePath,
				CapturedAt:  capturedAt,
				Memory:      memory,
			})
		}
		memories = append(memories, memory)
	}

	return ScanResult{
		Snapshot: api.AgentSnapshot{
			NodeName:   s.NodeName,
			CapturedAt: capturedAt,
			Containers: snapshots,
		},
		ContainersFound: len(entries),
		Mapped:          mapped,
		Unmapped:        unmapped,
		TotalMemory:     model.SumMemory("containers", memories),
		WalkError:       walkErr,
	}, nil
}

func PostSnapshot(ctx context.Context, collectorURL string, snapshot api.AgentSnapshot) error {
	if strings.TrimSpace(collectorURL) == "" {
		return fmt.Errorf("collector URL is required")
	}

	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("encode snapshot: %w", err)
	}

	endpoint := strings.TrimRight(collectorURL, "/") + "/api/v1/snapshots"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create snapshot request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("post snapshot: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("post snapshot: status=%d body=%q", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil
}

func LooksLikeCgroupDir(dir string) bool {
	for _, name := range []string{"memory.current", "memory.stat"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.IsDir() {
			return false
		}
	}

	return true
}

func chooseString(value string, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
