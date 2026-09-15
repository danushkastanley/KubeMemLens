# OOM qualification fixtures

These fixtures are separate from the standard agent, chart and release images.
No fixture grants programme-loading or OOM-test approval.

`test_semantics.py` compiles the exact OOM hook source against test-only native
helpers in an immutable, capability-free compiler image. It checks victim-first
filtering, unrelated invokers, already-exiting paths, missing context, scope,
ring failure, deadlines and context cleanup. It does not load BPF or prove kernel
helper, verifier or attachment behaviour.

`workload.c` is a separate static allocation fixture. `verify` touches and checks
2 MiB, then releases it. `consume` attempts at most 96 MiB in fixed 1 MiB chunks,
touching each native page. Reaching that ceiling or receiving an allocation error
without a confirmed kernel OOM is a failed qualification result. The fixture
prints no PID, command, target identifier or memory content.

Only run `consume` after the exact candidate and test scope are accepted. Use an
owned synthetic Pod with a 32 or 64 MiB memory limit, finite CPU/PID/duration bounds,
dropped capabilities, RuntimeDefault seccomp, a read-only root and no API token.
Use `restartPolicy: Never` and an active deadline to prevent repeated OOM loops.
Verify the actual limit/swap environment and start consumption only after the
owned programme's control map is active. Do not change `oom_score_adj`,
`memory.oom.group`, global OOM policy or host limits to manufacture a pass.

Confirm OOM through the observed kernel decision and the owned Pod/cgroup evidence;
an exit code or allocation failure alone is not proof. Preserve target replacement
as termination, never silently trace a restarted instance. Compare paths and process
fields only in memory, retaining counts and fixed fixture-match results.

For isolation, trigger bounded OOM in a non-selected Pod in another owned namespace
while tracing an idle selected target. Check captured owned programme/link/map
identities after every run. Remove only the owned fixture Pods and namespaces.

Global-OOM testing requires a separate disposable Linux VM with predeclared caps.
The shared Docker VM and host must never be exhausted. A cgroup-local OOM or a
Kubernetes MemoryPressure fixture does not qualify global kernel OOM handling.

Compile with the recorded immutable local GCC/musl image, no network or
capabilities, a read-only source mount and a bounded writable output directory.
Build twice with `-std=c11 -Wall -Wextra -Werror -O2 -static`, fixed hardening flags
and no build ID; compare binary bytes before constructing the pinned-base image.
The image remains local qualification material and is not approved for publication.
