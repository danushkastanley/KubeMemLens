package verifier

import "github.com/cilium/ebpf/btf"

// ValidateABI checks the function types used by the fixed probes. It does not
// prove symbol attachability. The caller must hash the exact kernel BTF source,
// bind its boot/architecture, and validate argument fetching with native fixtures.
func ValidateABI(spec *btf.Spec) error {
	if spec == nil {
		return ErrObservation
	}
	var check, finalize *btf.Func
	if spec.TypeByName("bpf_check", &check) != nil || spec.TypeByName("bpf_vlog_finalize", &finalize) != nil {
		return ErrObservation
	}
	checkType, ok := btf.UnderlyingType(check.Type).(*btf.FuncProto)
	if !ok || !integerType(checkType.Return, btf.Signed) {
		return ErrObservation
	}
	finalType, ok := btf.UnderlyingType(finalize.Type).(*btf.FuncProto)
	if !ok || !integerType(finalType.Return, btf.Signed) || len(finalType.Params) != 2 {
		return ErrObservation
	}
	logPointer, ok := btf.UnderlyingType(finalType.Params[0].Type).(*btf.Pointer)
	if !ok {
		return ErrObservation
	}
	log, ok := btf.UnderlyingType(logPointer.Target).(*btf.Struct)
	if !ok || log.Name != "bpf_verifier_log" {
		return ErrObservation
	}
	sizePointer, ok := btf.UnderlyingType(finalType.Params[1].Type).(*btf.Pointer)
	if !ok || !integerType(sizePointer.Target, btf.Unsigned) {
		return ErrObservation
	}
	return nil
}

func integerType(value btf.Type, encoding btf.IntEncoding) bool {
	integer, ok := btf.UnderlyingType(value).(*btf.Int)
	return ok && integer.Size == 4 && integer.Encoding == encoding
}
