package incident

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/api"
	"github.com/danushkastanley/kube-memlens/internal/model"
)

// realMarker is in every generated cluster name and in nothing a redacted
// bundle may keep: no alias, category, JSON key or tool version contains it.
const realMarker = "zq"

func realName(kind string, index int) string {
	return fmt.Sprintf("%s%s%03d%s", realMarker, kind, index, realMarker)
}

type generatedOwner struct{ ownerKind, ownerName, workloadKind, workloadName string }

// generatedOwnerFor covers the owner shapes the collector reports: none, a
// ReplicaSet under a Deployment, a StatefulSet that is both, a static Pod owned
// by its Node, and a Job without a reported workload.
func generatedOwnerFor(rng *rand.Rand, node string, index int) generatedOwner {
	switch rng.IntN(5) {
	case 1:
		return generatedOwner{"ReplicaSet", realName("rs", index), "Deployment", realName("deploy", index/2)}
	case 2:
		name := realName("sts", index/2)
		return generatedOwner{"StatefulSet", name, "StatefulSet", name}
	case 3:
		return generatedOwner{"Node", node, "Node", node}
	case 4:
		return generatedOwner{"Job", realName("job", index), "", ""}
	}
	return generatedOwner{}
}

func generatedDeepBundle(seed uint64, size uint8) api.IncidentBundle {
	rng := rand.New(rand.NewPCG(seed, uint64(size)))
	at := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	namespaces, nodes, pods := 1+rng.IntN(3), 1+rng.IntN(3), 1+int(size%12)
	bundle := api.IncidentBundle{SchemaVersion: api.CurrentIncidentSchemaVersion, CapturedAt: at, ToolVersion: "test", Redacted: true}
	for index := range nodes {
		bundle.Nodes = append(bundle.Nodes, api.NodeSnapshotStatus{NodeName: realName("node", index), CapturedAt: at, Environment: api.NodeEnvironment{CgroupVersion: "v2", ContainerRuntimes: []string{"containerd"}}})
	}
	for index := range pods {
		namespace, name, node := realName("ns", rng.IntN(namespaces)), realName("pod", rng.IntN(pods)), realName("node", rng.IntN(nodes))
		uid := realName("uid", index)
		owner := generatedOwnerFor(rng, node, index)
		runtimeClass := realName("runtime", rng.IntN(2))
		pod := api.PodSnapshot{Namespace: namespace, PodName: name, PodUID: uid, NodeName: node, CapturedAt: at,
			Memory: model.MemoryBreakdown{Name: namespace + "/" + name, TotalBytes: uint64(1+index) << 20},
			Context: api.PodContext{QoSClass: "Burstable", Phase: "Running", LastTerminationReason: "OOMKilled", OwnerKind: owner.ownerKind, OwnerName: owner.ownerName,
				WorkloadKind: owner.workloadKind, WorkloadName: owner.workloadName, RuntimeClassName: runtimeClass, Labels: map[string]string{"app": realName("label", index)}}}
		for _, container := range rng.Perm(3)[:1+rng.IntN(3)] {
			containerName := realName("ctr", container)
			pod.Containers = append(pod.Containers, api.ContainerSnapshot{Namespace: namespace, PodName: name, PodUID: uid, ContainerName: containerName,
				ContainerID: "containerd://" + realName("cid", index*3+container), NodeName: node, CgroupPath: "/kubepods/" + uid, CapturedAt: at,
				Memory: model.MemoryBreakdown{Name: namespace + "/" + name + "/" + containerName, TotalBytes: 1 << 20},
				Context: api.ContainerContext{QoSClass: "Burstable", PodPhase: "Running", OwnerKind: owner.ownerKind, OwnerName: owner.ownerName,
					WorkloadKind: owner.workloadKind, WorkloadName: owner.workloadName, RuntimeClassName: runtimeClass, Labels: map[string]string{"app": realName("label", index)}}})
		}
		bundle.Pods = append(bundle.Pods, pod)
		if rng.IntN(2) == 0 {
			bundle.Histories = append(bundle.Histories, api.PodHistory{Namespace: namespace, PodName: name, PodUID: uid, NodeName: node, Points: []api.MemoryHistoryPoint{{CapturedAt: at, TotalBytes: 1 << 20}}})
		}
	}
	return bundle
}

func cloneDeepBundle(t *testing.T, bundle api.IncidentBundle) api.IncidentBundle {
	t.Helper()
	data, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	var clone api.IncidentBundle
	if err := json.Unmarshal(data, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

// writeRedacted runs the capture path: Redact, then the validating writer,
// then the strict reader. It returns the bytes a user would share. Redact
// edits slices in place, so it works on a copy.
func writeRedacted(t *testing.T, input api.IncidentBundle) ([]byte, api.IncidentBundle) {
	t.Helper()
	bundle := cloneDeepBundle(t, input)
	Redact(&bundle)
	path := filepath.Join(t.TempDir(), "incident.json")
	if err := Write(nil, path, false, bundle); err != nil {
		t.Fatalf("write redacted bundle: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	document, err := Read(path)
	if err != nil || document.Deep == nil {
		t.Fatalf("read redacted bundle: %v", err)
	}
	return data, *document.Deep
}

// aliasLedger checks that one real value has one alias in a file and that two
// real values never share one.
type aliasLedger struct {
	t        *testing.T
	forward  map[string]string
	backward map[string]string
}

func (l aliasLedger) record(prefix, key, value, got string) {
	l.t.Helper()
	if value == "" {
		if got != "" {
			l.t.Fatalf("empty %s became %q", prefix, got)
		}
		return
	}
	if !alias(got, prefix) {
		l.t.Fatalf("%s %q became %q, not a %s-N alias", prefix, value, got, prefix)
	}
	id := prefix + "\x00" + key
	if prior, ok := l.forward[id]; ok && prior != got {
		l.t.Fatalf("%s %q has two aliases: %q and %q", prefix, value, prior, got)
	}
	if prior, ok := l.backward[got]; ok && prior != id {
		l.t.Fatalf("alias %q names two values: %q and %q", got, prior, id)
	}
	l.forward[id], l.backward[got] = got, id
}

// reference mirrors the documented scheme: owners and workloads are aliased by
// lower-case kind, and a Node owner shares its Node's alias.
func (l aliasLedger) reference(kind, namespace, name, got string) {
	prefix := strings.ToLower(kind)
	if prefix == "node" {
		l.record("node", name, name, got)
		return
	}
	l.record(prefix, namespace+"/"+name, name, got)
}

func checkDeepAliases(t *testing.T, original, redacted api.IncidentBundle) {
	t.Helper()
	l := aliasLedger{t: t, forward: map[string]string{}, backward: map[string]string{}}
	for i, before := range original.Pods {
		after := redacted.Pods[i]
		l.record("namespace", before.Namespace, before.Namespace, after.Namespace)
		l.record("pod", before.Namespace+"/"+before.PodName, before.PodName, after.PodName)
		l.record("node", before.NodeName, before.NodeName, after.NodeName)
		l.reference(before.Context.OwnerKind, before.Namespace, before.Context.OwnerName, after.Context.OwnerName)
		l.reference(before.Context.WorkloadKind, before.Namespace, before.Context.WorkloadName, after.Context.WorkloadName)
		if after.PodUID != "" || after.Memory.Name != "" || after.Context.RuntimeClassName != "" || after.Context.Labels != nil {
			t.Fatalf("Pod %d kept an identifier: %#v", i, after)
		}
		for j, c := range before.Containers {
			got := after.Containers[j]
			l.record("namespace", c.Namespace, c.Namespace, got.Namespace)
			l.record("pod", c.Namespace+"/"+c.PodName, c.PodName, got.PodName)
			l.record("container", c.Namespace+"/"+c.PodName+"/"+c.ContainerName, c.ContainerName, got.ContainerName)
			l.record("node", c.NodeName, c.NodeName, got.NodeName)
			l.reference(c.Context.OwnerKind, c.Namespace, c.Context.OwnerName, got.Context.OwnerName)
			l.reference(c.Context.WorkloadKind, c.Namespace, c.Context.WorkloadName, got.Context.WorkloadName)
			if got.PodUID != "" || got.ContainerID != "" || got.CgroupPath != "" || got.Memory.Name != "" || got.Context.RuntimeClassName != "" || got.Context.Labels != nil {
				t.Fatalf("container %d/%d kept an identifier: %#v", i, j, got)
			}
		}
	}
	for i, before := range original.Histories {
		after := redacted.Histories[i]
		l.record("namespace", before.Namespace, before.Namespace, after.Namespace)
		l.record("pod", before.Namespace+"/"+before.PodName, before.PodName, after.PodName)
		l.record("node", before.NodeName, before.NodeName, after.NodeName)
		if after.PodUID != "" {
			t.Fatalf("history %d kept its Pod UID", i)
		}
	}
	for i, before := range original.Nodes {
		l.record("node", before.NodeName, before.NodeName, redacted.Nodes[i].NodeName)
	}
}

func leakedLine(data []byte) string {
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, realMarker) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// FuzzRedactedDeepIncidentKeepsNoClusterName is the issue 322 property: a
// redacted schema 1 or 2 capture holds no real namespace, Pod, Node, container,
// owner or workload name anywhere, every such field is an alias, aliases are
// consistent within the file, and redaction is deterministic and idempotent.
func FuzzRedactedDeepIncidentKeepsNoClusterName(f *testing.F) {
	for _, seed := range []uint64{1, 7, 322} {
		f.Add(seed, uint8(seed))
	}
	f.Fuzz(func(t *testing.T, seed uint64, size uint8) {
		original := generatedDeepBundle(seed, size)
		for _, schema := range []int{api.LegacySchemaVersion, api.CurrentIncidentSchemaVersion} {
			bundle := original
			bundle.SchemaVersion = schema
			data, redacted := writeRedacted(t, bundle)
			if line := leakedLine(data); line != "" {
				t.Fatalf("schema %d redacted capture (seed %d, size %d) keeps a cluster name: %s", schema, seed, size, line)
			}
			if !redacted.Redacted || len(redacted.Pods) != len(original.Pods) || len(redacted.Histories) != len(original.Histories) || len(redacted.Nodes) != len(original.Nodes) {
				t.Fatalf("schema %d redaction changed the bundle shape", schema)
			}
			checkDeepAliases(t, original, redacted)
			again, _ := writeRedacted(t, bundle)
			twice, _ := writeRedacted(t, redacted)
			if !bytes.Equal(data, again) || !bytes.Equal(data, twice) {
				t.Fatalf("schema %d redaction is not deterministic and idempotent (seed %d, size %d)", schema, seed, size)
			}
		}
	})
}
