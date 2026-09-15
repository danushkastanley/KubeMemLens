# Worker test environments

`make check-trace-worker` separately verifies the nested SDK worker module,
materialises or validates its checksum-pinned patched SDK, runs race tests and
vet, and cross-compiles for Linux amd64 and arm64. The dedicated
`trace-worker.yml` workflow runs these non-loading checks with read-only repository
permissions. Parent-module `go test ./...` does not cross the nested module boundary.

This job does not sign or load programmes. Offline candidate-object preparation
tests require `KML_FILECACHE_OBJECTS` (four-object file/cache build) and
`KML_OOM_OBJECTS` (six-object build for OOM tests) and remain part of local candidate verification,
along with native C regression tests, signed reproduction, dependency/security
review and the explicitly accepted kernel-test matrix. A green worker job is not
runtime, supply-chain acceptance or provider qualification.

The parent optional module's normal `go test -race ./...` command now runs its
worker installation, containment and launcher tests with static child fixtures.
The parent remains race-instrumented. Production ELF validation continues to
reject interpreter/shared-library dependencies.

`internal/testworker` is imported only by tests. When a test executable is dynamic
or race-instrumented, it compiles the same test package once with CGO disabled and
race instrumentation off. Compilation is bounded at two minutes, uses the current
local Go toolchain, readonly module resolution and no module proxy. `TestMain`
removes the owned fixture directory after tests finish. Compiler/fixture diagnostics
are capped at 16 KiB and are separate from discarded production worker stderr.

The fixture uses `GOTMPDIR` for executable build output when configured. A container
may keep `/tmp` non-executable while providing a dedicated executable test-build
directory. Go coverage also executes a helper from `GOCACHE`, so coverage runs
need an executable test cache. Keep cache and compiler scratch space separate and
bounded; clear only the owned test cache between large race/vet/cross-build stages
when needed. This is test execution setup, not a worker deployment profile.

## Seccomp coverage

Dedicated fixture children add a test baseline that denies `getppid`, then apply
the real worker restriction. Both filters remain stacked. Tests verify that the
baseline denial survives, sockets/process creation/exec are denied, existing and
new Go threads work, and the invalid syscall ABI flag terminates the child.

A separate local negative run started with inherited seccomp mode 0. It verified
that production restriction rejects the absent baseline before the fixture adds
its own filter. The production requirement for an existing container filter is
unchanged. No test removes or relaxes an inherited filter.

## Recorded local checks

The full optional-module race suite passed on native Linux arm64 using Go 1.27.1
with a local GCC/musl test toolchain. The runtime container had no network,
capabilities or writable source/module-cache mounts. Test build directories were
temporary. The retained public candidate bundle was mounted read-only; private
signing keys were not mounted.

These checks exercise protocol/installation/launcher fixtures and OS restrictions.
They do not load incident BPF, qualify another architecture, complete the SDK's
incident profile, or replace the programme acceptance and independent review gates.

The OOM candidate additionally tests victim-first hook semantics, five fixed cgroup
reads, scope/missing-context accounting, v3 framing and rich-summary size, exact
per-kind launcher selection, and control-service-only Kubernetes context. Positive
OOM launcher fixtures require `KML_REVIEW_BUNDLE` to name the six-object signed
public bundle. SDK preparation tests inspect and prepare accepted ELF data with
BPF denied and without starting readers; they are not load or attachment evidence.
