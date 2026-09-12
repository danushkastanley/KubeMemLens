package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/danushkastanley/kube-memlens/internal/volumecontext"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func workloadWireFixture() WorkloadVolumeContext {
	now := time.Now().UTC()
	pod := PodVolumeContext{TypeMeta: metav1.TypeMeta{APIVersion: MemoryAPIGroup + "/" + MemoryAPIVersion, Kind: "PodVolumeContext"}, ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "app-pod", UID: "pod-uid"}, Context: volumecontext.View{SchemaVersion: 1, Namespace: "team", PodName: "app-pod", Volumes: []volumecontext.NamedVolume{}}}
	groups, _ := volumecontext.GroupWorkload([]volumecontext.View{pod.Context}, now)
	return WorkloadVolumeContext{TypeMeta: metav1.TypeMeta{APIVersion: MemoryAPIGroup + "/" + MemoryAPIVersion, Kind: "WorkloadVolumeContext"}, ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "app", UID: "workload-uid"}, ObservedAt: now, Workload: WorkloadSnapshot{Namespace: "team", Name: "app", Kind: "Deployment", PodCount: 1, Pods: []PodSnapshot{{Namespace: "team", PodName: "app-pod", PodUID: "pod-uid"}}}, PodVolumes: []PodVolumeContext{pod}, Filesystems: groups}
}

func TestWorkloadVolumeDecoderAndScopeBounds(t *testing.T) {
	value := workloadWireFixture()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded WorkloadVolumeContext
	if json.Unmarshal(data, &decoded) != nil || ValidateWorkloadVolumeContext(decoded, time.Now().UTC()) != nil {
		t.Fatal("valid workload rejected")
	}
	for _, body := range []string{
		strings.Replace(string(data), `"kind":"WorkloadVolumeContext"`, `"kind":"WorkloadVolumeContext","Kind":"WorkloadVolumeContext"`, 1),
		strings.Replace(string(data), `"podVolumes":[`, `"podVolumes":[`+strings.Repeat(`{},`, 33), 1),
		strings.Replace(string(data), `"observedAt":`, `"private-field":"secret","observedAt":`, 1),
		string(data) + ` {}`, strings.Repeat(" ", volumecontext.MaxPageBytes+1),
	} {
		if json.Unmarshal([]byte(body), &decoded) == nil {
			t.Fatal("malformed workload accepted")
		}
	}
	for name, mutate := range map[string]func(*WorkloadVolumeContext){
		"wrong namespace":    func(v *WorkloadVolumeContext) { v.Workload.Pods[0].Namespace = "other" },
		"replaced Pod":       func(v *WorkloadVolumeContext) { v.Workload.Pods[0].PodUID = "replacement" },
		"invented count":     func(v *WorkloadVolumeContext) { v.Workload.PodCount = 2 },
		"duplicate scope":    func(v *WorkloadVolumeContext) { v.PodVolumes = append(v.PodVolumes, v.PodVolumes[0]) },
		"unsupported owner":  func(v *WorkloadVolumeContext) { v.Workload.Kind = "Pod" },
		"future observation": func(v *WorkloadVolumeContext) { v.ObservedAt = time.Now().Add(time.Hour) },
	} {
		t.Run(name, func(t *testing.T) {
			v := workloadWireFixture()
			mutate(&v)
			if ValidateWorkloadVolumeContext(v, time.Now().UTC()) == nil {
				t.Fatal("invalid composition accepted")
			}
		})
	}
}

func TestVolumeBindingReferencesRequireSchemaSix(t *testing.T) {
	value := workloadWireFixture().PodVolumes[0]
	value.Context.Volumes = []volumecontext.NamedVolume{{VolumeName: "data", EvidenceID: strings.Repeat("a", 64), FilesystemID: strings.Repeat("b", 64)}}
	for _, schema := range []int{4, 5, 6} {
		projected := PodVolumeContextForSchema(value, schema)
		body, _ := json.Marshal(projected)
		if strings.Contains(string(body), "evidenceID") != (schema == 6) || strings.Contains(string(body), "filesystemID") != (schema == 6) {
			t.Fatalf("schema %d exposed wrong references", schema)
		}
	}
	if value.Context.Volumes[0].EvidenceID == "" {
		t.Fatal("projection mutated source")
	}
}
