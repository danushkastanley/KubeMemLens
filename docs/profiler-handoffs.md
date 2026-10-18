# Language-profiler handoffs

`kubectl memlens recommend pod NAME -n NAMESPACE --profilers` adds reviewed
guidance for an explicitly declared runtime workflow. Workload recommendations
also accept `--profilers`. JSON and YAML use recommendation schema **4**;
ordinary recommendations keep schema **1**. Use `--volumes` separately.
The standard TUI recommendation action shows the same profiler guidance.

The first supported workflow is **Go heap profiles collected through an existing
approved process**. KubeMemLens only displays commands to analyse local files.
It does not execute commands, attach to a process, collect or upload a profile,
enable an endpoint, or grant profiling access.

## Declare the intended container

The workload owner can add this Pod-template label through normal deployment
configuration, after verifying that container `app` runs Go and has an approved
heap-profile collection workflow:

```yaml
metadata:
  labels:
    profiling.kubememlens.io/app: go-heap-pprof-v1
```

The suffix must exactly match the container name. A sidecar does not inherit
another container's declaration. This is owner-supplied metadata, not independent
process detection. Image names, commands, ports and RuntimeClass are not used to
guess a language. Do not put endpoints or credentials in these labels.

Guidance requires fresh, complete deep evidence and a current RSS-heavy diagnosis
for that container. Both Pod and container observations must be at most 30 seconds
old and no more than 5 seconds in the future, with matching identities and a
Running Pod phase. Unknown declarations, stale or partial observations and other
diagnoses produce explicit abstention. Higher-priority OOM/pressure investigation
remains primary. Anonymous-memory dominance alone does not establish heap growth
or a leak. Restricted mode cannot supply this evidence.

The adapter handles at most 64 Pods and 64 containers per request. Select a Pod
when a workload exceeds that bound. Results are sorted by namespace, Pod and
container; no workload metadata is interpolated into commands.

## Review and analyse existing profiles

Obtain the workload owner's permission and follow its existing collection runbook.
Collection can affect latency and memory. Agree a budget; do not expose endpoints,
force GC or change sampling rates merely to follow this handoff. No additional
Kubernetes permission is installed by this feature.

Put the approved binary profile at `./heap.pprof` in a private local directory.
For comparison, use an earlier profile from the same process/build at
`./baseline.pprof`. Review the output's prerequisites and these commands, then
run them yourself with a trusted Go toolchain:

```sh
go tool pprof -top -sample_index=inuse_space ./heap.pprof
go tool pprof -top -sample_index=alloc_space ./heap.pprof
go tool pprof -top -sample_index=inuse_space -base ./baseline.pprof ./heap.pprof
```

`inuse_space` estimates retained sampled Go allocations at the last completed GC;
`alloc_space` is cumulative allocation, not retained memory. Check process
continuity, build, timestamps and comparable demand before interpreting a
difference. Go heap samples do not explain every native allocation, stack, mapping
or cgroup charge. See the official [Go diagnostics guide](https://go.dev/doc/diagnostics),
[heap profile semantics](https://pkg.go.dev/runtime/pprof) and
[existing HTTP profile handlers](https://pkg.go.dev/net/http/pprof).

## Privacy and rollback

Profiles can contain internal symbols, source paths and labels. Restrict local
file access and retention; do not upload them with incident captures. Public
captures strip Pod/container labels, including profiler declarations and any
misplaced endpoint credentials. Replay therefore cannot recover a declaration.
Recommendation documents contain only recognised static guidance and existing
authorised target names; raw declaration values are never echoed.

Remove the declaration to stop offering the handoff after the next fresh snapshot,
or omit `--profilers` for the existing CLI document. Core diagnoses, memory
measurements, resource settings and cluster permissions are unchanged.
