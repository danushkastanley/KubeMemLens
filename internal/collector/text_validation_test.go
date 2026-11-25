package collector

import (
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
)

func textSnapshot(now time.Time) api.AgentSnapshot {
	return api.AgentSnapshot{
		SchemaVersion: api.CurrentSnapshotSchemaVersion, NodeName: "node-a", CapturedAt: now,
		Environment: api.NodeEnvironment{CgroupVersion: "v2", CgroupDriver: "systemd", ContainerRuntimes: []string{"containerd"}},
		Containers: []api.ContainerSnapshot{{
			Namespace: "team-a", PodName: "api", PodUID: "uid", ContainerName: "app", ContainerID: "id-a", CgroupPath: "/kubepods/pod/id-a",
			Context: api.ContainerContext{OwnerKind: "ReplicaSet", OwnerName: "api-1", WorkloadKind: "Deployment", WorkloadName: "api", Labels: map[string]string{"app": "api"}},
		}},
	}
}

// Each of these values is shown in a terminal, written to a log line or used
// as a metric label. A newline or an escape sequence in any of them must be
// refused at ingestion.
func TestSnapshotTextFieldsRejectControlCharacters(t *testing.T) {
	now := time.Now().UTC()
	fields := map[string]func(*api.AgentSnapshot, string){
		"nodeName":                         func(s *api.AgentSnapshot, v string) { s.NodeName = v },
		"environment.cgroupVersion":        func(s *api.AgentSnapshot, v string) { s.Environment.CgroupVersion = v },
		"environment.cgroupDriver":         func(s *api.AgentSnapshot, v string) { s.Environment.CgroupDriver = v },
		"environment.memoryPressureStatus": func(s *api.AgentSnapshot, v string) { s.Environment.MemoryPressureStatus = v },
		"environment.containerRuntimes[0]": func(s *api.AgentSnapshot, v string) { s.Environment.ContainerRuntimes[0] = v },
		"namespace":                        func(s *api.AgentSnapshot, v string) { s.Containers[0].Namespace = v },
		"podName":                          func(s *api.AgentSnapshot, v string) { s.Containers[0].PodName = v },
		"podUID":                           func(s *api.AgentSnapshot, v string) { s.Containers[0].PodUID = v },
		"containerName":                    func(s *api.AgentSnapshot, v string) { s.Containers[0].ContainerName = v },
		"containerID":                      func(s *api.AgentSnapshot, v string) { s.Containers[0].ContainerID = v },
		"cgroupPath":                       func(s *api.AgentSnapshot, v string) { s.Containers[0].CgroupPath = v },
		"context.qosClass":                 func(s *api.AgentSnapshot, v string) { s.Containers[0].Context.QoSClass = v },
		"context.lastTerminationReason":    func(s *api.AgentSnapshot, v string) { s.Containers[0].Context.LastTerminationReason = v },
		"context.podPhase":                 func(s *api.AgentSnapshot, v string) { s.Containers[0].Context.PodPhase = v },
		"context.ownerKind":                func(s *api.AgentSnapshot, v string) { s.Containers[0].Context.OwnerKind = v },
		"context.ownerName":                func(s *api.AgentSnapshot, v string) { s.Containers[0].Context.OwnerName = v },
		"context.workloadKind":             func(s *api.AgentSnapshot, v string) { s.Containers[0].Context.WorkloadKind = v },
		"context.workloadName":             func(s *api.AgentSnapshot, v string) { s.Containers[0].Context.WorkloadName = v },
		"context.nodeMemoryPressure":       func(s *api.AgentSnapshot, v string) { s.Containers[0].Context.NodeMemoryPressure = v },
		"context.runtimeClassName":         func(s *api.AgentSnapshot, v string) { s.Containers[0].Context.RuntimeClassName = v },
		"context.labels key":               func(s *api.AgentSnapshot, v string) { s.Containers[0].Context.Labels = map[string]string{v: "x"} },
		"context.labels value":             func(s *api.AgentSnapshot, v string) { s.Containers[0].Context.Labels = map[string]string{"app": v} },
	}
	if err := ValidateSnapshot(textSnapshot(now), now, DefaultHandlerOptions(time.Minute)); err != nil {
		t.Fatalf("clean snapshot refused: %v", err)
	}
	for field, set := range fields {
		for name, value := range map[string]string{"newline": "a\nb", "escape": "a\x1b[2Jb", "NUL": "a\x00b", "invalid UTF-8": "a\xffb", "right-to-left override": "a\u202eb"} {
			snapshot := textSnapshot(now)
			set(&snapshot, value)
			err := ValidateSnapshot(snapshot, now, DefaultHandlerOptions(time.Minute))
			if err == nil || !strings.Contains(err.Error(), field) {
				t.Errorf("%s with %s: err = %v", field, name, err)
			}
		}
	}
}

func TestSnapshotTextFieldsAcceptPrintableNonASCII(t *testing.T) {
	now := time.Now().UTC()
	snapshot := textSnapshot(now)
	snapshot.Containers[0].Context.Labels = map[string]string{"team": "plätze-équipe"}
	if err := ValidateSnapshot(snapshot, now, DefaultHandlerOptions(time.Minute)); err != nil {
		t.Fatalf("printable non-ASCII label refused: %v", err)
	}
}
