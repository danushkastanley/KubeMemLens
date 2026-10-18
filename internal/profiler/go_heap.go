package profiler

func goHeap() *Handoff {
	return &Handoff{
		Profile: GoHeapProfile, Runtime: "Go",
		Basis: "Explicit owner declaration in the matching container's Pod label; no process inspection.",
		Prerequisites: []string{
			"Ask the workload owner to confirm this container runs Go and already has an approved heap-profile collection workflow.",
			"Obtain permission to collect and read profiles through that workflow; KubeMemLens grants no endpoint, exec, attach or port-forward access.",
			"Place the approved binary heap profile at ./heap.pprof in a private local directory. For comparison, place an earlier comparable profile at ./baseline.pprof.",
			"Use the trusted Go toolchain and profiles from the intended process and build. Review these POSIX-shell commands before running them yourself.",
		},
		Risks: []string{
			"Collection can affect workload latency and memory. Agree its budget with the owner; do not enable endpoints, force GC or change sampling rates from this guidance.",
			"Profiles can expose internal function names, source paths and labels. Restrict local file access, follow retention policy and do not upload them with incident captures.",
		},
		Commands: []string{
			"go tool pprof -top -sample_index=inuse_space ./heap.pprof",
			"go tool pprof -top -sample_index=alloc_space ./heap.pprof",
			"go tool pprof -top -sample_index=inuse_space -base ./baseline.pprof ./heap.pprof",
		},
		Verification: []string{
			"Confirm profile timestamps, process continuity, build and comparable workload demand before using the baseline command.",
			"inuse_space estimates retained sampled Go allocations at the last completed GC; alloc_space measures cumulative allocation, not retained memory.",
			"Compare repeated profiles with the cgroup observation window. One profile or anonymous-memory dominance cannot prove a leak.",
			"Go heap samples do not explain all RSS, native allocations, stacks, mappings or cgroup charges; a gap is not an attribution result.",
		},
		References: []string{"https://go.dev/doc/diagnostics", "https://pkg.go.dev/runtime/pprof", "https://pkg.go.dev/net/http/pprof"},
	}
}
