package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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
	Snapshot              api.AgentSnapshot
	ContainersFound       int
	Mapped                int
	Unmapped              int
	InfrastructureCgroups int
	TotalMemory           model.MemoryBreakdown
	RootFallback          bool
	WalkError             error
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
	entries, walkErr := (cgroup.Walker{Root: s.CgroupRoot}).WalkContext(ctx)
	if errors.Is(walkErr, context.Canceled) || errors.Is(walkErr, context.DeadlineExceeded) {
		return ScanResult{}, walkErr
	}
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
				SchemaVersion: api.CurrentSnapshotSchemaVersion,
				NodeName:      s.NodeName,
				CapturedAt:    capturedAt,
				Environment: api.NodeEnvironment{
					CgroupVersion:          "v2",
					CgroupDriver:           detectCgroupDriver(nil),
					ContainerRuntimes:      []string{"unknown"},
					CgroupReadErrors:       boolCount(walkErr != nil),
					NodeContextAvailable:   idx.NodeContext.Available,
					MemoryPressureStatus:   idx.NodeContext.MemoryPressureStatus,
					MemoryPressureSince:    idx.NodeContext.MemoryPressureSince,
					MemoryAllocatableBytes: idx.NodeContext.MemoryAllocatableBytes,
					MemoryAllocatableKnown: idx.NodeContext.MemoryAllocatableKnown,
				},
			},
			TotalMemory:  breakdown,
			RootFallback: true,
			WalkError:    walkErr,
		}, nil
	}

	type mapping struct {
		ref kube.PodRef
		ok  bool
	}
	mappings := make([]mapping, len(entries))
	mappedParents := map[string]struct{}{}
	for i, entry := range entries {
		ref, ok := idx.Lookup(entry.ContainerID, entry.PodUID)
		mappings[i] = mapping{ref: ref, ok: ok}
		if ok {
			mappedParents[filepath.Dir(entry.RelativePath)] = struct{}{}
		}
	}

	snapshots := make([]api.ContainerSnapshot, 0, len(entries))
	memories := make([]model.MemoryBreakdown, 0, len(entries))
	mapped := 0
	unmapped := 0
	infrastructure := 0
	runtimes := map[string]struct{}{}
	for i, entry := range entries {
		select {
		case <-ctx.Done():
			return ScanResult{}, ctx.Err()
		default:
		}

		memory := entry.Memory
		ref, ok := mappings[i].ref, mappings[i].ok
		if ok {
			mapped++
			ref.Context.NodeMemoryPressure = idx.NodeContext.MemoryPressureStatus
			ref.Context.NodeMemoryAllocatable = idx.NodeContext.MemoryAllocatableBytes
			ref.Context.NodeMemoryAllocatableKnown = idx.NodeContext.MemoryAllocatableKnown
			runtimes[chooseString(ref.Runtime, "unknown")] = struct{}{}
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
				Context:       ref.Context,
				Memory:        memory,
			})
		} else if _, siblingMapped := mappedParents[filepath.Dir(entry.RelativePath)]; siblingMapped {
			// CRI Pod sandboxes are charged in a sibling cgroup but are not
			// exposed as Kubernetes containers. Avoid presenting that runtime
			// infrastructure as an unknown workload container.
			infrastructure++
			continue
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
			SchemaVersion: api.CurrentSnapshotSchemaVersion,
			NodeName:      s.NodeName,
			CapturedAt:    capturedAt,
			Environment: api.NodeEnvironment{
				CgroupVersion:            "v2",
				CgroupDriver:             detectCgroupDriver(entries),
				ContainerRuntimes:        sortedStrings(runtimes),
				CgroupReadErrors:         boolCount(walkErr != nil),
				NodeContextAvailable:     idx.NodeContext.Available,
				MemoryPressureStatus:     idx.NodeContext.MemoryPressureStatus,
				MemoryPressureSince:      idx.NodeContext.MemoryPressureSince,
				MemoryAllocatableBytes:   idx.NodeContext.MemoryAllocatableBytes,
				MemoryAllocatableKnown:   idx.NodeContext.MemoryAllocatableKnown,
				WorkloadContextAvailable: idx.WorkloadContextAvailable,
				WorkloadContextErrors:    idx.WorkloadContextErrors,
			},
			Containers: snapshots,
		},
		ContainersFound:       len(entries),
		Mapped:                mapped,
		Unmapped:              unmapped,
		InfrastructureCgroups: infrastructure,
		TotalMemory:           model.SumMemory("containers", memories),
		WalkError:             walkErr,
	}, nil
}

func detectCgroupDriver(entries []cgroup.Entry) string {
	for _, entry := range entries {
		if strings.Contains(entry.RelativePath, ".slice") || strings.Contains(entry.RelativePath, ".scope") {
			return "systemd"
		}
	}
	return "cgroupfs"
}

func sortedStrings(values map[string]struct{}) []string {
	if len(values) == 0 {
		return []string{"unknown"}
	}
	items := make([]string, 0, len(values))
	for value := range values {
		items = append(items, value)
	}
	sort.Strings(items)
	return items
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
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
