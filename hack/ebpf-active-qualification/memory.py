"""Conservative paired working-set accounting, not a qualification verdict."""
from resources import working_set
from samples import require, validate_window

TRACER_ROLES = {"node", "api"}
NORMAL_WORKING_SET_LIMIT = 64 << 20


def paired_memory(control, enabled, seconds, shared_roles):
    """Validate both streams; bind candidate/lifetimes/schedule externally first.

    The control has no tracer services. Retain every enabled sample, including
    startup and gaps. For each shared role, use its lowest control working set
    and never credit a reduction against another role's added memory. This gives
    a conservative observed-cgroup upper bound without assuming phase alignment.
    Kernel allocation attribution and unobserved processes remain separate gates.
    """
    require(type(shared_roles) is set and "selected" in shared_roles
            and not shared_roles & TRACER_ROLES, "invalid paired memory roles")
    validate_window(control, seconds, shared_roles)
    validate_window(enabled, seconds, shared_roles | TRACER_ROLES)
    floors = {role: min(working_set(row["groups"][role]) for row in control)
              for role in sorted(shared_roles)}
    tracer, additional, totals = [], [], []
    for row in enabled:
        installed = sum(working_set(row["groups"][role]) for role in TRACER_ROLES)
        shared = sum(max(0, working_set(row["groups"][role]) - floors[role])
                     for role in shared_roles)
        tracer.append(installed)
        additional.append(shared)
        totals.append(installed + shared)
    peak = max(totals)
    return {"scope": "paired-observed-cgroup-memory-only", "sampleCount": len(enabled),
            "controlWorkingSetFloorBytes": floors,
            "installationPeakWorkingSetBytes": max(tracer),
            "sharedRolePeakAdditionalWorkingSetBytes": max(additional),
            "conservativePeakIncrementalWorkingSetBytes": peak,
            "normalObservedWorkingSetBudgetPassed": peak <= NORMAL_WORKING_SET_LIMIT,
            "qualification": "incomplete: external provenance, kernel allocation attribution and remaining protocol gates required"}
