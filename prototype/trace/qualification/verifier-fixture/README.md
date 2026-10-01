# Fixed verifier calibration fixture

This optional qualification package provides exactly three non-attaching
`BPF_PROG_LOAD` cases: accepted with logging disabled, accepted with a fixed
4 KiB log buffer, and rejected with that same bounded buffer. Each programme has
two instructions. The rejected case leaves the return register uninitialised.
There are no helper calls, jumps, maps, links, pins or arbitrary bytecode inputs.

The fixture reads the syscall's `log_true_size` output independently of the
observer and returns only numeric log lengths, errno, the owned programme ID and
a monotonic syscall duration upper bound. Log text is cleared and never emitted.
Accepted programme FDs are closed immediately. `Absent` checks those specific IDs
afterwards and never deletes a surviving object.

This is a separate fixture, not a change to the accepted worker or SDK policy.
Ordinary unit tests validate inputs and ABI layout without loading programmes.
The runtime controller must explicitly authorise calibration, restrict the child
to BPF/PERFMON capabilities, enforce core/process/memory/CPU/time limits, start it
inside a newly owned cgroup and verify that group's membership before capture.
At most the three fixed cases per selected and excluded group are permitted per
attempt. A failure must retain evidence and stop rather than resize a buffer,
retry a load or broaden privileges.

Before treating the observer as validated, compare each selected call's observed
duration and finalised log size with the independently returned syscall result;
require a measured non-zero log case, correct rejection and zero excluded-group
events. Verify zero remaining owned programmes, probes, helper processes and
cgroups. These fixed cases and exclusion checks have passed locally through the
[calibration runner](../verifier-calibration/README.md). Full observer lifetime
and campaign qualification remain separate checks.

The 144-byte syscall layout ends at `log_true_size`, following the
[Linux BPF UAPI](https://github.com/torvalds/linux/blob/v6.12/include/uapi/linux/bpf.h).
That field reports required log size including the terminator; it is distinct
from the supplied buffer size and from the bytes actually written into it.
