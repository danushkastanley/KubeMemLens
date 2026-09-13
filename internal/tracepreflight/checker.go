package tracepreflight

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"time"
	"unicode/utf8"
)

type State string

const (
	Supported   State = "supported"
	Degraded    State = "degraded"
	Unsupported State = "unsupported"
)

type Reason string

const (
	Available               Reason = "available"
	PlatformUnsupported     Reason = "platform_unsupported"
	ArchitectureUnsupported Reason = "architecture_unsupported"
	KernelUnsupported       Reason = "kernel_unsupported"
	CgroupMissing           Reason = "cgroup_v2_missing"
	BTFMissing              Reason = "btf_missing"
	BTFUnreported           Reason = "btf_unreported"
	BTFInvalid              Reason = "btf_invalid"
	CapabilityMissing       Reason = "capability_missing"
	ExcessPrivilege         Reason = "excess_privilege"
	PolicyDenied            Reason = "policy_denied"
	PolicyUnreported        Reason = "policy_unreported"
	BPFFSAbsent             Reason = "bpffs_absent_pins_disabled"
	EngineMismatch          Reason = "engine_identity_mismatch"
	EngineUnreported        Reason = "engine_identity_unreported"
	OwnedOrphan             Reason = "owned_orphan"
	OwnershipUncertain      Reason = "ownership_uncertain"
	InventoryDenied         Reason = "inventory_denied"
	InventoryLimit          Reason = "inventory_limit"
	ProgrammeMissing        Reason = "programme_type_missing"
	HelperMissing           Reason = "helper_missing"
	MapMissing              Reason = "map_type_missing"
	HookMissing             Reason = "hook_missing"
	HookUnreported          Reason = "hook_unreported"
	PrerequisiteMissing     Reason = "prerequisite_missing"
	ProbeFailed             Reason = "probe_failed"
	ProbeTimeout            Reason = "probe_timeout"
	ProbeBusy               Reason = "probe_busy"
	ProbeInvalid            Reason = "probe_invalid"
)

type Check struct {
	ID     ID     `json:"id"`
	State  State  `json:"state"`
	Reason Reason `json:"reason"`
	Value  string `json:"value,omitempty"`
}

type Report struct {
	SchemaVersion int       `json:"schemaVersion"`
	Scope         string    `json:"scope"`
	ProfileDigest string    `json:"profileDigest"`
	TraceApproval string    `json:"traceApproval"`
	CapturedAt    time.Time `json:"capturedAt"`
	State         State     `json:"state"`
	Checks        []Check   `json:"checks"`
}

// Prober must honour cancellation and release all transient resources before
// returning. An OS adapter must also enforce a process-level deadline around
// kernel calls that cannot be cancelled through context.
type Prober interface {
	Probe(context.Context, ID) Check
}

// Checker serialises checks for one node-side instance and uses a fixed profile.
// It never accepts caller-selected helpers, programme types or attach points.
type Checker struct {
	prober  Prober
	gate    chan struct{}
	timeout time.Duration
}

func New(prober Prober, timeout time.Duration) (*Checker, error) {
	if prober == nil || timeout <= 0 || timeout > 15*time.Second {
		return nil, errors.New("preflight requires a prober and a timeout at most 15 seconds")
	}
	return &Checker{prober: prober, timeout: timeout, gate: make(chan struct{}, 1)}, nil
}

func (c *Checker) Run(ctx context.Context) Report {
	profile := Baseline()
	result := Report{SchemaVersion: SchemaVersion, Scope: profile.Scope,
		ProfileDigest: profile.Digest(), TraceApproval: "pending-custom-programme-freeze", CapturedAt: time.Now().UTC(), State: Supported}
	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	default:
		result.State = Degraded
		result.Checks = []Check{{ID: Slot, State: Degraded, Reason: ProbeBusy}}
		return result
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	canProbe := true
	for _, id := range profile.Checks {
		check := Check{ID: id, State: Degraded, Reason: PrerequisiteMissing}
		switch {
		case ctx.Err() != nil:
			check.Reason = ProbeTimeout
		case needsKernelProbe(id) && !canProbe:
		default:
			check = c.prober.Probe(ctx, id)
			if ctx.Err() != nil {
				check = Check{ID: id, State: Degraded, Reason: ProbeTimeout}
			}
		}
		if !validCheck(id, check) {
			check = Check{ID: id, State: Unsupported, Reason: ProbeInvalid}
		}
		if check.State != Supported && id != BPFFS && id != LSM {
			canProbe = false
		}
		result.Checks = append(result.Checks, check)
		result.State = combine(result.State, check.State)
	}
	return result
}

func combine(a, b State) State {
	if a == Unsupported || b == Unsupported {
		return Unsupported
	}
	if a == Degraded || b == Degraded {
		return Degraded
	}
	return Supported
}

func validCheck(id ID, c Check) bool {
	if id != Slot && !slices.Contains(Baseline().Checks, id) {
		return false
	}
	if c.ID != id || !utf8.ValidString(c.Value) || len(c.Value) > MaxValueBytes {
		return false
	}
	for _, r := range c.Value {
		if r < 0x20 || r > 0x7e {
			return false
		}
	}
	if c.State != Supported && c.State != Degraded && c.State != Unsupported {
		return false
	}
	switch c.Reason {
	case Available, PlatformUnsupported, ArchitectureUnsupported, KernelUnsupported, CgroupMissing,
		BTFMissing, BTFUnreported, BTFInvalid, CapabilityMissing, ExcessPrivilege, PolicyDenied, PolicyUnreported,
		BPFFSAbsent, EngineMismatch, EngineUnreported, OwnedOrphan, OwnershipUncertain,
		InventoryDenied, InventoryLimit, ProgrammeMissing, HelperMissing, MapMissing,
		HookMissing, HookUnreported, PrerequisiteMissing, ProbeFailed, ProbeTimeout, ProbeBusy, ProbeInvalid:
		return (c.State == Supported) == (c.Reason == Available)
	default:
		return false
	}
}

func Encode(report Report) ([]byte, error) {
	profile := Baseline()
	if report.SchemaVersion != SchemaVersion || report.Scope != profile.Scope || report.ProfileDigest != profile.Digest() || report.TraceApproval != "pending-custom-programme-freeze" || report.CapturedAt.IsZero() {
		return nil, errors.New("preflight report does not match the selected profile")
	}
	if len(report.Checks) == 0 || len(report.Checks) > MaxChecks {
		return nil, errors.New("preflight report exceeds check limit")
	}
	state := Supported
	seen := make(map[ID]bool, len(report.Checks))
	for _, check := range report.Checks {
		if !validCheck(check.ID, check) || seen[check.ID] {
			return nil, errors.New("invalid preflight check")
		}
		seen[check.ID] = true
		state = combine(state, check.State)
	}
	if report.State != state {
		return nil, errors.New("preflight state contradicts its checks")
	}
	if !seen[Slot] && len(report.Checks) != len(profile.Checks) {
		return nil, errors.New("preflight checks are incomplete")
	}
	if seen[Slot] && (len(report.Checks) != 1 || (report.Checks[0].Reason != ProbeBusy && report.Checks[0].Reason != ProbeTimeout && report.Checks[0].Reason != ProbeFailed)) {
		return nil, errors.New("invalid preflight admission result")
	}
	data, err := json.Marshal(report)
	if err != nil {
		return nil, err
	}
	if len(data) > MaxReportBytes {
		return nil, errors.New("preflight report exceeds byte limit")
	}
	return data, nil
}

func Decode(data []byte) (Report, error) {
	if len(data) > MaxReportBytes {
		return Report{}, errors.New("preflight report exceeds byte limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var report Report
	if err := decoder.Decode(&report); err != nil {
		return Report{}, errors.New("invalid preflight report")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Report{}, errors.New("invalid trailing preflight data")
	}
	if _, err := Encode(report); err != nil {
		return Report{}, err
	}
	return report, nil
}

func Failed(reason Reason) Report {
	profile := Baseline()
	return Report{SchemaVersion: SchemaVersion, Scope: profile.Scope, ProfileDigest: profile.Digest(), TraceApproval: "pending-custom-programme-freeze",
		CapturedAt: time.Now().UTC(), State: Degraded, Checks: []Check{{ID: Slot, State: Degraded, Reason: reason}}}
}
