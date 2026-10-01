# Bounded verifier calibration runner

This Linux-only command validates the numeric verifier observer with six fixed
loads: no-log, logged and rejected cases in a selected cgroup, then the same
cases in an excluded cgroup. Programmes are never attached or pinned. This is
qualification tooling, not a production command or performance qualification.

Build reviewed source for the target architecture from `prototype/trace`:

```sh
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -mod=readonly -trimpath -buildvcs=false \
  -o verifier-calibration ./qualification/verifier-calibration
```

The administrator-owned controller must journal a fresh 32-character lowercase
hexadecimal owner token, exact boot/BTF hash, binary/source hashes and the initial
absence of that owner's tracefs definitions. Before invoking the command, create
two new empty cgroups under the already delegated cgroup-v2 root, named
`kml-vcal-<Owner>-selected` and `kml-vcal-<Owner>-excluded`. Record their actual
inodes. Set each to 64 MiB memory, zero swap, 16 processes and one CPU quota
(`cpu.max=100000 100000`). Do not alter shared controller settings to make a test
pass. Refuse existing groups or changed bounds.

Supply exactly these JSON fields on stdin. Values below illustrate the schema;
the hashes, boot ID and inodes must come from the owned host:

```json
{
  "Owner": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "BTF": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
  "Boot": "11111111-1111-1111-1111-111111111111",
  "SelectedGroup": "/sys/fs/cgroup/kml-vcal-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-selected",
  "OtherGroup": "/sys/fs/cgroup/kml-vcal-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-excluded",
  "SelectedInode": 10,
  "OtherInode": 20,
  "CPUs": [0, 1]
}
```

The CPU roster must contain every online CPU in ascending order, with at least
two and at most 64 CPUs, all below 1024. The runner exercises the first and last
CPU. Duplicate/unknown/missing fields, trailing input, unbounded documents and
unowned paths are rejected before native work.

```sh
./verifier-calibration --acknowledge-fixed-verifier-calibration \
  < private-config.json > private-result.json
```

The parent has a 15-second hard lifetime. Children start atomically inside their
assigned cgroups, have an eight-second lifetime and parent-death signal, and
restrict every Go thread to BPF/PERFMON capabilities plus no-new-privileges.
Core dumps are disabled; CPU time is limited to two seconds and FDs to 64.
Each child accepts at most three fixed cases. Log buffers never exceed 4 KiB.

Success requires exactly three selected verifier calls and nine samples, matching
syscall-reported log sizes and rejection, positive durations inside syscall time
bounds, zero excluded samples, zero lost/missed events, and absent owned programme
IDs after FD close. The runner closes perf FDs before removing its probes. Output
is private numeric evidence; it contains owned programme IDs but no verifier text,
task identities, programme instructions or kernel addresses.

An external controller is mandatory: after any exit or timeout, wait for its
owned helper processes to exit, reconcile only exact journalled probe definitions,
confirm both cgroups empty and inode-matched, record memory events, and remove
those groups and the hash-bound helper. Never clear a global probe registry,
delete foreign definitions or kill an unbound process. Preserve failed output;
do not retry loads or enlarge limits within an attempt.

A local development run matched required log sizes 0, 98 and 188 bytes against
the syscall oracle, with zero excluded events. These sizes are observations,
not expected constants for future kernels. Full observer cancellation/lifetime
validation, campaign integration and provider qualification remain separate gates.
