package cgroup

import (
	"strings"
	"testing"
)

// Cgroup paths come from the node's filesystem. Whatever they contain, the
// extractors must return nothing or a well-formed identity that the path
// really holds, and a cgroup nested inside a Pod's container must keep
// belonging to that container.
func FuzzExtractIdentityFromPath(f *testing.F) {
	for _, seed := range []string{
		"", "/", "kubepods.slice/kubepods-pod" + testPodUIDSystemd + ".slice/cri-containerd-" + testContainerID + ".scope",
		"/kubepods/burstable/pod" + testPodUID + "/" + testContainerID,
		"/system.slice/docker-" + testContainerID + ".scope", "pod", "podpodpod", "\x00/\xff/pod" + testPodUID,
		"/kubepods/pod" + testPodUID + "/" + testContainerID + "/system.slice/docker-" + strings.Repeat("f", 64) + ".scope",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, path string) {
		if len(path) > 4096 {
			return
		}
		uid := ExtractPodUIDFromPath(path)
		if uid != "" && (strings.ToLower(uid) != uid || strings.ContainsAny(uid, "_/") || (len(uid) != 32 && len(uid) != 36)) {
			t.Fatalf("Pod UID %q from path %q is not a lower-case UID", uid, path)
		}
		id := ExtractContainerIDFromPath(path)
		if id == "" {
			return
		}
		if !isHexID(id, 12, 128) || strings.ToLower(id) != id {
			t.Fatalf("container ID %q from path %q is not a lower-case hexadecimal ID", id, path)
		}
		if !strings.Contains(strings.ToLower(path), id) {
			t.Fatalf("container ID %q does not appear in path %q", id, path)
		}
		// A Pod's container owns everything below it.
		if uid != "" {
			nested := path + "/system.slice/docker-" + strings.Repeat("e", 64) + ".scope"
			if got := ExtractContainerIDFromPath(nested); got != id {
				t.Fatalf("cgroup nested in container %q of path %q was given to %q", id, path, got)
			}
		}
	})
}
