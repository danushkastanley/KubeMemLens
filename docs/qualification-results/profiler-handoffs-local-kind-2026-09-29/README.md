# Local Go profiler-handoff verification — 29 September 2026

The first static handoff is `go-heap-pprof-v1`, explicitly declared by a workload
owner. It analyses existing local files. No profile collection or execution path
was added to KubeMemLens. [Operator guide](../../profiler-handoffs.md).

Host formatting, support/scale/provider/terminal/release/community contracts,
full unit/coverage/race suites, vet, vulnerability scan and builds passed.
The scan reported zero reachable vulnerabilities, plus two imported-package and
one required-module findings outside called code; these were not suppressed.
Native Linux Go 1.27.1 module verification, affected-package race/vet checks and
Linux arm64/amd64 CLI builds passed in a bounded, networkless test container.
The Linux-only trace worker is unchanged; its tests are not a new trace
qualification result from this feature.

A real Go process wrote baseline and held-allocation heap profiles. Each of the
three exact static commands ran successfully under the test harness, and each
identified the fixture allocation site. Profile contents remain private.

The existing two-Node kind Kubernetes 1.37 environment ran the standard collector
and a restricted Go fixture Pod with two 64 MiB containers. The final client passed:

- authenticated text/JSON/YAML recommendations and unchanged default schema 1;
- guidance only for the explicitly labelled app, with sidecar abstention;
- denied reader, declaration removal and read-grant revocation;
- private mode-0600 incident capture with profiler labels removed;
- real 80x24 and 160x35 terminal recommendation, command visibility, final caveat,
  first/last navigation, return to dashboard and successful exit;
- removal of owned Helm resources, APIService, namespace and fixtures, with both
  existing Nodes Ready afterwards.

The first terminal harness completed navigation but missed its final success
marker because of a single-line Tcl `expect` block. The corrected multiline
harness passed both sizes; the original attempt was retained. An initial
implementation check also showed action results were truncated at the terminal
height. The product now wraps and scrolls those results using the existing
viewport, with a regression test reaching the final verification text.

[Summary](summary.json) contains sanitised checks and private receipt hashes.
[Source](source.json) binds the changed production sources, final live client and
fixture/collector image tags. No endpoint credentials, profile contents, tokens
or private workload identities are published. Managed-provider testing remains
deferred to the final combined campaign. This record is our own testing; it is
not independent review or adoption evidence.
